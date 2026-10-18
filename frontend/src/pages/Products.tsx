import { useCallback, useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Package, Plus, Search } from 'lucide-react'
import { api, qs, type Page } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import type { Product } from '../lib/types'
import { Topbar, Pill, Loading, Empty, Tabs, Drawer, Field } from '../components/ui'
import { fmtAgo, productStatus, pipelineStatus, platformName } from '../lib/format'

type Tab = 'all' | 'sampling' | 'pending' | 'listed' | 'archived'

export function Products() {
  const { me, can } = useAuth()
  const nav = useNavigate()
  const [items, setItems] = useState<Product[] | null>(null)
  const [tab, setTab] = useState<Tab>('all')
  const [q, setQ] = useState('')
  const [shop, setShop] = useState('')
  const [mine, setMine] = useState(false)
  const [creating, setCreating] = useState(false)
  const shops = me?.shops || []
  const shopName = (id: number) => shops.find(s => s.id === id)?.name || `店铺 ${id}`

  const load = useCallback(() => {
    api<Page<Product>>('/api/products' + qs({ q, shop_id: shop, mine: mine ? 1 : undefined, page_size: 200 }))
      .then(r => setItems(r.items)).catch(() => setItems([]))
  }, [q, shop, mine])
  useEffect(() => { const t = setTimeout(load, q ? 250 : 0); return () => clearTimeout(t) }, [load, q])

  const counts = useMemo(() => {
    const c: Record<string, number> = { all: 0 }
    for (const p of items || []) { c[p.status] = (c[p.status] || 0) + 1; if (p.status !== 'archived') c.all++ }
    return c
  }, [items])
  const shown = (items || []).filter(p => (tab === 'all' ? p.status !== 'archived' : p.status === tab))

  return (
    <>
      <Topbar title="商品" sub={<>{counts.all ?? 0} 款在用<span className="sep" />{counts.pending ?? 0} 款待上架<span className="sep" />{counts.sampling ?? 0} 款拍样中</>}>
        {can('product.edit') && <button className="btn btn-primary" onClick={() => setCreating(true)}><Plus />新建商品</button>}
      </Topbar>
      <div className="scroll"><main className="content">
        <Tabs value={tab} onChange={setTab} items={[
          { key: 'all', label: '全部', count: counts.all ?? 0 },
          { key: 'sampling', label: '拍样中', count: counts.sampling ?? 0 },
          { key: 'pending', label: '待上架', count: counts.pending ?? 0 },
          { key: 'listed', label: '已上架', count: counts.listed ?? 0 },
          { key: 'archived', label: '已归档', count: counts.archived ?? 0 }
        ]} />
        <div className="toolbar">
          {shops.length > 1 && (
            <select className="input" style={{ width: 160 }} value={shop} onChange={e => setShop(e.target.value)}>
              <option value="">全部店铺</option>
              {shops.map(s => <option key={s.id} value={s.id}>{s.name}</option>)}
            </select>
          )}
          <div className="segmented"><button className={mine ? '' : 'active'} onClick={() => setMine(false)}>所有人</button><button className={mine ? 'active' : ''} onClick={() => setMine(true)}>我建的</button></div>
          <span className="spacer" />
          <label className="field" style={{ width: 240 }}><Search /><input placeholder="搜索款名" value={q} onChange={e => setQ(e.target.value)} /></label>
        </div>
        <section className="panel">
          {!items ? <Loading /> : shown.length === 0 ? (
            <Empty title={q || shop || mine ? '没有符合条件的商品' : '还没有商品'} desc={can('product.edit') ? '点右上角「新建商品」，再上传实拍图。' : undefined} />
          ) : (
            <div className="table-wrap"><table className="table">
              <thead><tr><th>商品</th><th>状态</th><th>店铺版本</th><th className="num">素材</th><th>更新</th></tr></thead>
              <tbody>{shown.map(p => {
                const ps = productStatus[p.status]
                return (
                  <tr key={p.id} className="clickable" onClick={() => nav(`/products/${p.id}`)}>
                    <td><div className="name-cell">
                      {p.cover_url ? <img className="cover" src={p.cover_url} alt="" /> : <span className="cover"><Package /></span>}
                      <div style={{ minWidth: 0 }}><div className="cell-title ellipsis">{p.name}</div><div className="cell-sub mono">#{p.id}{p.info?.category ? ` · ${p.info.category}` : ''}</div></div>
                    </div></td>
                    <td><Pill kind={ps?.pill || 'pill-neutral'}>{ps?.label || p.status}</Pill></td>
                    <td><span className="shop-chips">{(p.listings || []).length === 0 ? <span className="t4">—</span> : p.listings.map(l => {
                      const st = pipelineStatus[l.pipeline_status] || pipelineStatus.draft
                      return <span key={l.id} className="tag" title={st.label}>{shopName(l.shop_id)} · {st.label}</span>
                    })}</span></td>
                    <td className="num mono">{p.asset_count ?? 0}</td>
                    <td className="t3">{fmtAgo(p.updated_at)}</td>
                  </tr>
                )
              })}</tbody>
            </table></div>
          )}
        </section>
      </main></div>
      {creating && <CreateProduct onClose={() => setCreating(false)} onDone={id => nav(`/products/${id}`)} />}
    </>
  )
}

function CreateProduct({ onClose, onDone }: { onClose: () => void; onDone: (id: number) => void }) {
  const { me, can } = useAuth()
  const toast = useToast()
  const shops = (me?.shops || []).filter(s => !s.archived)
  const [name, setName] = useState('')
  const [shopIds, setShopIds] = useState<number[]>(shops.length === 1 ? [shops[0].id] : [])
  const [info, setInfo] = useState({ category: '', size: '', metal: '', price: '', highlights: '' })
  const [cost, setCost] = useState('')
  const [supplier, setSupplier] = useState('')
  const [busy, setBusy] = useState(false)
  const set = (k: keyof typeof info) => (e: { target: { value: string } }) => setInfo(v => ({ ...v, [k]: e.target.value }))
  const submit = async () => {
    setBusy(true)
    try {
      const body: Record<string, unknown> = {
        name: name.trim(), shop_ids: shopIds,
        info: { ...info, price: info.price ? Number(info.price) : undefined }
      }
      if (can('product.cost')) {
        if (cost) body.cost = { unit: Number(cost), currency: 'CNY' }
        if (supplier) body.supplier = { name: supplier }
      }
      const r = await api<{ product: { id: number } }>('/api/products', { body })
      toast('已新建，接着上传实拍图', 'ok')
      onDone(r.product.id)
    } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  return (
    <Drawer title="新建商品" onClose={onClose} foot={<>
      <button className="btn btn-secondary" onClick={onClose}>取消</button>
      <button className="btn btn-primary" disabled={busy || !name.trim()} onClick={submit}>创建</button>
    </>}>
      <Field label="款名" req><input className="input" autoFocus value={name} onChange={e => setName(e.target.value)} placeholder="如：珠蝶" /></Field>
      <Field label="上架到哪些店" hint="每个店会建一个上架版本，文案和成品图各店分开。以后还能再加。">
        <div className="checks">{shops.map(s => (
          <label key={s.id}><input type="checkbox" className="check" checked={shopIds.includes(s.id)}
            onChange={e => setShopIds(v => e.target.checked ? [...v, s.id] : v.filter(x => x !== s.id))} />{s.name}<span className="t4 fs-12">{platformName[s.platform]}</span></label>
        ))}{shops.length === 0 && <span className="t3 fs-12">你还没有分到店铺，可以先建商品，以后再加店铺版本。</span>}</div>
      </Field>
      <div className="form-row">
        <Field label="品类"><input className="input" value={info.category} onChange={set('category')} placeholder="耳钉 / 项链…" /></Field>
        <Field label="尺寸"><input className="input" value={info.size} onChange={set('size')} placeholder="10–11 mm" /></Field>
      </div>
      <div className="form-row">
        <Field label="配件"><input className="input" value={info.metal} onChange={set('metal')} placeholder="925 银针" /></Field>
        <Field label="售价"><input className="input mono" inputMode="decimal" value={info.price} onChange={set('price')} placeholder="69" /></Field>
      </div>
      <Field label="卖点" hint="其他信息（珍珠类型、颜色等）AI 会看照片补全，补不了的会提醒你。">
        <textarea className="textarea" rows={3} value={info.highlights} onChange={set('highlights')} placeholder="配锆石蝴蝶结，送礼" />
      </Field>
      {can('product.cost') && <div className="form-row">
        <Field label="拿货成本（元）"><input className="input mono" inputMode="decimal" value={cost} onChange={e => setCost(e.target.value)} /></Field>
        <Field label="货源"><input className="input" value={supplier} onChange={e => setSupplier(e.target.value)} placeholder="诸暨 · 某某" /></Field>
      </div>}
    </Drawer>
  )
}
