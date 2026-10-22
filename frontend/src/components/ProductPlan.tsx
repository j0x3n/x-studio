import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { Save, RotateCcw } from 'lucide-react'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { planApi, type Plan, type PlanOverrides, type ProductPlan as PP } from '../lib/views'
import type { Product } from '../lib/types'
import { Empty, Loading, Pill } from './ui'
import { OverrideFields, usePlanOptions } from './PlanEditor'

const EMPTY_CFG = { template_set: {}, models: {}, scene_id: null, detail_layout: '' }

// 商品的出图方案：引用一个方案（改方案会影响这款之后出的图），再按需单独覆盖几项
export function ProductPlan({ product }: { product: Product }) {
  const { can } = useAuth()
  const toast = useToast()
  const options = usePlanOptions()
  const [d, setD] = useState<PP | null>(null)
  const [plans, setPlans] = useState<Plan[] | null>(null)
  const [planId, setPlanId] = useState<number | null>(null)
  const [ov, setOv] = useState<PlanOverrides>({})
  const [busy, setBusy] = useState(false)
  const edit = can('product.edit')
  const load = useCallback(() => planApi.ofProduct(product.id).then(r => { setD(r); setPlanId(r.plan?.id ?? null); setOv(r.overrides) }).catch(e => toast(errText(e), 'error')), [product.id, toast])
  useEffect(() => { load() }, [load])
  useEffect(() => { planApi.list().then(setPlans).catch(() => setPlans([])) }, [])
  if (!d || !plans || !options) return <section className="panel"><Loading /></section>
  const cat = product.category?.key
  const fits = plans.filter(p => !p.categories.length || !cat || p.categories.includes(cat))
  const chosen = planId === d.plan?.id ? d.plan : plans.find(p => p.id === planId) || null
  const base = chosen || EMPTY_CFG
  const dirty = planId !== (d.plan?.id ?? null) || JSON.stringify(ov) !== JSON.stringify(d.overrides)
  const ownCount = Object.keys(ov.template_set || {}).length + Object.keys(ov.models || {}).length + ('scene_id' in ov ? 1 : 0) + ('detail_layout' in ov ? 1 : 0)
  const save = async () => {
    setBusy(true)
    try { await planApi.setProduct(product.id, { plan_id: planId, plan_overrides: ov }); toast('出图方案已保存，之后新出的图按这个来', 'ok'); load() }
    catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  return <section className="panel">
    <div className="panel-head">
      <div className="titles">
        <div className="panel-title inline">出图方案{ownCount > 0 && <Pill kind="pill-info" dot={false}>单独改了 {ownCount} 项</Pill>}</div>
        <div className="panel-sub">引用方案：改方案会影响引用它的商品之后出的图；只想这款不一样的地方在下面单独指定</div>
      </div>
      {can('creative.template') && <Link className="btn btn-ghost btn-sm" to="/templates?mode=plans">管理方案</Link>}
      {edit && dirty && <button className="btn btn-ghost btn-sm" disabled={busy} onClick={() => { setPlanId(d.plan?.id ?? null); setOv(d.overrides) }}><RotateCcw />还原</button>}
      {edit && <button className="btn btn-primary btn-sm" disabled={busy || !dirty} onClick={save}><Save />保存</button>}
    </div>
    <div className="panel-pad stack">
      <div className="row" style={{ gap: 8 }}>
        <select className="input" style={{ width: 260 }} disabled={!edit} value={planId || 0} onChange={e => setPlanId(Number(e.target.value) || null)} aria-label="引用方案">
          <option value={0}>不引用方案（按店铺设置）</option>
          {fits.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
          {d.plan && !fits.some(p => p.id === d.plan!.id) && <option value={d.plan.id}>{d.plan.name}{d.plan.archived ? '（已归档）' : ''}</option>}
        </select>
        {chosen?.note && <span className="t3 fs-12 ellipsis">{chosen.note}</span>}
        {chosen?.archived && <Pill kind="pill-neutral" dot={false}>方案已归档</Pill>}
      </div>
      {!fits.length && !d.plan && <Empty title="还没有适合这个品类的方案" desc="在「模板库 → 出图方案」里建一个，把常用的模板、模特、场景组合存起来。" />}
      <OverrideFields base={base} value={ov} onChange={setOv} options={options} categories={cat ? [cat] : undefined} disabled={!edit} />
    </div>
  </section>
}
