import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { Archive, ArchiveRestore, ClipboardList, Plus, Trash2 } from 'lucide-react'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { useCategories } from '../lib/categories'
import { fmtAgo } from '../lib/format'
import { slotName } from '../lib/creative'
import { MODEL_SLOTS, planApi, type ModelSlotKey, type Plan, type PlanInput } from '../lib/views'
import { Drawer, Empty, Field, Loading, Pill, Topbar } from './ui'
import { PlanFields, usePlanOptions, type PlanOptions } from './PlanEditor'

// 出图方案 = 分镜脚本：哪些位置用哪个模板、模特位用哪几位模特、场景和详情版式。商品引用方案，改方案影响引用它的商品之后出的图。
export function CreativePlans({ switcher }: { switcher: ReactNode }) {
  const { can } = useAuth()
  const toast = useToast()
  const { items: cats } = useCategories()
  const options = usePlanOptions()
  const [archived, setArchived] = useState(false)
  const [items, setItems] = useState<Plan[] | null>(null)
  const [editing, setEditing] = useState<Plan | 'new' | null>(null)
  const edit = can('creative.template')
  const load = useCallback(() => planApi.list(archived).then(r => setItems(archived ? r.filter(p => p.archived) : r)).catch(e => { toast(errText(e), 'error'); setItems([]) }), [archived, toast])
  useEffect(() => { setItems(null); load() }, [load])
  const catName = (k: string) => cats?.find(c => c.key === k)?.name || k
  return <>
    <Topbar crumbs={['模板库', '出图方案']}>{switcher}{edit && <button className="btn btn-primary" onClick={() => setEditing('new')}><Plus />新建方案</button>}</Topbar>
    <div className="scroll"><main className="content">
      <div className="toolbar">
        <div className="segmented"><button className={!archived ? 'active' : ''} onClick={() => setArchived(false)}>使用中</button><button className={archived ? 'active' : ''} onClick={() => setArchived(true)}>已归档</button></div>
        <span className="t3 fs-12">商品在「出图方案」页引用方案，还能单独改几项；改方案只影响之后新出的图</span>
        <span className="spacer" /><span className="t3 fs-12">{items ? `${items.length} 个方案` : ''}</span>
      </div>
      {!items || !options ? <Loading /> : !items.length ? <section className="panel"><Empty title={archived ? '没有归档的方案' : '还没有出图方案'} desc={archived ? undefined : '把一类商品常用的模板、模特、场景、详情版式存成方案，新款直接引用。'}>{edit && !archived && <button className="btn btn-secondary" onClick={() => setEditing('new')}><Plus />新建方案</button>}</Empty></section>
        : <section className="panel"><table className="table">
          <thead><tr><th>方案</th><th>品类</th><th>模特</th><th>场景</th><th>指定模板</th><th>更新</th></tr></thead>
          <tbody>{items.map(p => <tr key={p.id} className="clickable" onClick={() => setEditing(p)}>
            <td><div className="inline"><b>{p.name}</b>{p.archived && <Pill kind="pill-neutral" dot={false}>已归档</Pill>}</div>{p.note && <div className="t3 fs-12 ellipsis" style={{ maxWidth: 320 }}>{p.note}</div>}</td>
            <td className="fs-12">{p.categories.length ? p.categories.map(catName).join('、') : <span className="t3">全部</span>}</td>
            <td className="fs-12">{summaryModels(p, options) || <span className="t3">店铺设置</span>}</td>
            <td className="fs-12">{p.scene_id ? options.scenes.find(s => s.id === p.scene_id)?.name || `#${p.scene_id}` : <span className="t3">店铺默认</span>}</td>
            <td className="fs-12">{Object.keys(p.template_set).length ? Object.keys(p.template_set).map(slotName).join('、') : <span className="t3">—</span>}</td>
            <td className="t3 fs-12">{fmtAgo(p.updated_at)}</td>
          </tr>)}</tbody>
        </table></section>}
    </main></div>
    {editing && options && <PlanDrawer key={editing === 'new' ? 'new' : editing.id} plan={editing === 'new' ? undefined : editing} options={options} onClose={() => setEditing(null)} onSaved={p => { setEditing(p); load() }} onGone={() => { setEditing(null); load() }} />}
  </>
}

const summaryModels = (p: Plan, o: PlanOptions) => (Object.keys(MODEL_SLOTS) as ModelSlotKey[])
  .filter(k => p.models[k]?.length)
  .map(k => `${MODEL_SLOTS[k]}：${p.models[k]!.map(id => o.models.find(m => m.id === id)?.name || `#${id}`).join('、')}`).join('；')

function PlanDrawer({ plan, options, onClose, onSaved, onGone }: { plan?: Plan; options: PlanOptions; onClose: () => void; onSaved: (p: Plan) => void; onGone: () => void }) {
  const { can } = useAuth()
  const toast = useToast()
  const { items: cats } = useCategories()
  const edit = can('creative.template')
  const init = (p?: Plan): Required<Omit<PlanInput, 'archived'>> => ({ name: p?.name || '', note: p?.note || '', categories: p?.categories || [], template_set: p?.template_set || {}, models: p?.models || {}, scene_id: p?.scene_id ?? null, detail_layout: p?.detail_layout || '' })
  const [v, setV] = useState(init(plan))
  const [busy, setBusy] = useState(false)
  const dirty = JSON.stringify(v) !== JSON.stringify(init(plan))
  const save = async () => {
    setBusy(true)
    try { const p = plan ? await planApi.patch(plan.id, { ...v, name: v.name.trim() }) : await planApi.create({ ...v, name: v.name.trim() }); toast(plan ? '方案已保存，引用它的商品之后出图按新的来' : '方案已建好', 'ok'); onSaved(p) }
    catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const archive = async () => {
    if (!plan) return
    setBusy(true)
    try { onSaved(await planApi.patch(plan.id, { archived: !plan.archived })); toast(plan.archived ? '已恢复' : '已归档，已引用的商品照常能用', 'ok') }
    catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const remove = async () => {
    if (!plan || !confirm(`删除方案「${plan.name}」？有商品引用时只会归档。`)) return
    setBusy(true)
    try { const r = await planApi.remove(plan.id); toast(r.deleted ? '方案已删除' : '有商品在用，已改为归档', 'ok'); onGone() }
    catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const toggleCat = (k: string) => setV({ ...v, categories: v.categories.includes(k) ? v.categories.filter(x => x !== k) : [...v.categories, k] })
  return <Drawer title={<span className="inline"><ClipboardList size={16} />{plan ? plan.name : '新建出图方案'}</span>} width={680} onClose={onClose} foot={<>
    {plan && edit && <button className="btn btn-ghost" disabled={busy} onClick={archive}>{plan.archived ? <ArchiveRestore /> : <Archive />}{plan.archived ? '恢复使用' : '归档'}</button>}
    {plan && edit && <button className="btn btn-ghost danger-text" disabled={busy} onClick={remove}><Trash2 />删除</button>}
    <span className="grow" />
    <button className="btn btn-secondary" onClick={onClose}>{dirty && edit ? '取消' : '关闭'}</button>
    {edit && <button className="btn btn-primary" disabled={busy || !v.name.trim() || !dirty} onClick={save}>{busy ? '保存中…' : '保存'}</button>}
  </>}>
    <div className="stack">
      <div className="form-row">
        <Field label="方案名" req><input className="input" maxLength={60} autoFocus={!plan} disabled={!edit} placeholder="如：珍珠耳饰 · 米白画册" value={v.name} onChange={e => setV({ ...v, name: e.target.value })} /></Field>
        <Field label="说明"><input className="input" maxLength={300} disabled={!edit} placeholder="给同事看的，不进提示词" value={v.note} onChange={e => setV({ ...v, note: e.target.value })} /></Field>
      </div>
      {(cats?.length || 0) > 1 && <div className="form-field"><span className="label">适用品类</span>
        <div className="inline">{cats!.map(c => <label key={c.id} className="filter-opt"><input type="checkbox" className="check" disabled={!edit} checked={v.categories.includes(c.key)} onChange={() => toggleCat(c.key)} />{c.name}</label>)}<span className="help">都不勾 = 全部品类</span></div>
      </div>}
      <PlanFields value={v} onChange={c => setV({ ...v, ...c })} options={options} categories={v.categories} disabled={!edit} />
      <div className="help">一个模特位可以勾多位模特；没指定的项按店铺 AI 出图设置。</div>
    </div>
  </Drawer>
}
