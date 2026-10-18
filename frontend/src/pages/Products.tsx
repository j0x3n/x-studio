import { useCallback, useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Package, Plus, Search, Download, Sparkles, LayoutGrid, Table2, X } from 'lucide-react'
import { api, qs, downloadPost, type Page } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import type { Home, Listing, Product } from '../lib/types'
import { Topbar, Pill, Loading, Empty, Tabs, Drawer, Field } from '../components/ui'
import { fmtAgo, productStatus, platformName, initials } from '../lib/format'
import { useCategories, missingAttrs } from '../lib/categories'
import { AttrFields } from '../components/AttrFields'

type Tab = 'all' | 'sampling' | 'pending' | 'listed' | 'archived'
type Quick = '' | 'nophoto' | 'processing' | 'failed'

// 一键出图的 6 步，和后端 pipeline/jobs.go 的 steps 一致
const STEPS = ['check', 'classify', 'enhance', 'retouch', 'copy', 'render']
const STEP_NAME: Record<string, string> = { check: '检查照片', classify: '分拣', enhance: '补图', retouch: '修图', copy: '写文案', render: '出图' }
const QUICK_NAME: Record<Exclude<Quick, ''>, string> = { nophoto: '还没传照片', processing: '出图中', failed: '出图失败' }

// 店铺版本走到第几步、现在是什么状态
function lane(l: Listing): { done: number; cur: number; state: 'ok' | 'run' | 'bad' | 'none'; text: string } {
  const i = Math.max(0, STEPS.indexOf(l.pipeline_step))
  if (l.pipeline_status === 'ready') return { done: 6, cur: -1, state: 'ok', text: l.status === 'listed' ? '已上架' : '已出图' }
  if (l.pipeline_status === 'processing') return { done: i, cur: i, state: 'run', text: STEP_NAME[l.pipeline_step] || '出图中' }
  if (l.pipeline_status === 'failed') return { done: i, cur: i, state: 'bad', text: `${STEP_NAME[l.pipeline_step] || ''}失败` }
  return { done: 0, cur: -1, state: 'none', text: '未出图' }
}

function Lanes({ p, shopName }: { p: Product; shopName: (id: number) => string }) {
  if (!p.listings?.length) return <span className="t4">—</span>
  return (
    <div className="lanes">{p.listings.map(l => {
      const g = lane(l)
      return (
        <div key={l.id} className={'lane ' + g.state} title={`${shopName(l.shop_id)} · ${g.text}`}>
          <span className="lane-shop ellipsis">{shopName(l.shop_id)}</span>
          <span className="lane-bar">{STEPS.map((_, k) => <i key={k} className={k < g.done ? 'd' : k === g.cur ? 'c' : ''} />)}</span>
          <span className="lane-text">{g.text}</span>
        </div>
      )
    })}</div>
  )
}

export function Products() {
  const { me, can } = useAuth()
  const nav = useNavigate()
  const toast = useToast()
  const [items, setItems] = useState<Product[] | null>(null)
  const [home, setHome] = useState<Home | null>(null)
  const [tab, setTab] = useState<Tab>('all')
  const [quick, setQuick] = useState<Quick>('')
  const [q, setQ] = useState('')
  const [shop, setShop] = useState('')
  const [mine, setMine] = useState(false)
  const [cat, setCat] = useState('')
  const [view, setView] = useState<'table' | 'grid'>(() => { try { return localStorage.getItem('products.view') === 'grid' ? 'grid' : 'table' } catch { return 'table' } })
  const [sel, setSel] = useState<Set<number>>(new Set())
  const [busy, setBusy] = useState(false)
  const [creating, setCreating] = useState(false)
  const { items: cats } = useCategories(true)
  const shops = me?.shops || []
  // 只有一个店时显示平台名就够了
  const shopName = (id: number) => { const s = shops.find(x => x.id === id); return s ? (shops.length > 1 ? s.name : platformName[s.platform] || s.name) : `店铺 ${id}` }

  const load = useCallback(() => {
    api<Page<Product>>('/api/products' + qs({ q, shop_id: shop, category: cat, mine: mine ? 1 : undefined, page_size: 200 }))
      .then(r => setItems(r.items)).catch(() => setItems([]))
  }, [q, shop, cat, mine])
  useEffect(() => { const t = setTimeout(load, q ? 250 : 0); return () => clearTimeout(t) }, [load, q])
  useEffect(() => { api<Home>('/api/home').then(setHome).catch(() => setHome(null)) }, [])
  const pickView = (v: 'table' | 'grid') => { setView(v); try { localStorage.setItem('products.view', v) } catch { /* 无痕模式 */ } }

  const counts = useMemo(() => {
    const c: Record<string, number> = { all: 0, nophoto: 0, processing: 0, failed: 0 }
    for (const p of items || []) {
      c[p.status] = (c[p.status] || 0) + 1
      if (p.status === 'archived') continue
      c.all++
      if (!p.asset_count) c.nophoto++
      for (const l of p.listings || []) if (l.pipeline_status === 'processing' || l.pipeline_status === 'failed') c[l.pipeline_status]++
    }
    return c
  }, [items])
  const match = (p: Product) =>
    quick === 'nophoto' ? p.status !== 'archived' && !p.asset_count
      : quick ? (p.listings || []).some(l => l.pipeline_status === quick)
        : tab === 'all' ? p.status !== 'archived' : p.status === tab
  const shown = (items || []).filter(match)
  const picked = shown.filter(p => sel.has(p.id))
  const toggle = (id: number) => setSel(v => { const n = new Set(v); if (n.has(id)) n.delete(id); else n.add(id); return n })
  const allOn = shown.length > 0 && shown.every(p => sel.has(p.id))
  const pickTab = (t: Tab) => { setTab(t); setQuick('') }
  const owner = (p: Product) => p.created_by_name || (p.created_by === me?.user.id ? (me?.user.display_name || me?.user.username || '我') : '')

  const download = async () => {
    setBusy(true)
    try {
      const lists = await Promise.all(picked.map(p => api<Page<{ id: number }>>('/api/assets' + qs({ product_id: p.id, page_size: 500 }))))
      const ids = lists.flatMap(r => r.items.map(a => a.id))
      if (!ids.length) { toast('选中的商品还没有素材', 'error'); return }
      await downloadPost('/api/assets/download', { asset_ids: ids }, `素材_${picked.length}款.zip`)
    } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const render = async () => {
    const ids = picked.flatMap(p => (p.listings || []).filter(l => l.pipeline_status !== 'processing').map(l => l.id))
    if (!ids.length) { toast('选中的商品没有可以出图的店铺版本', 'error'); return }
    if (ids.length > 50) { toast('一次最多 50 个店铺版本，少选几款', 'error'); return }
    setBusy(true)
    try {
      const r = await api<{ items: { listing_id: number; job_id?: number; skipped_reason?: string }[] }>('/api/render/batch', { body: { listing_ids: ids } })
      const ok = r.items.filter(i => i.job_id).length
      toast(`已排队 ${ok} 个店铺版本${ok < r.items.length ? `，跳过 ${r.items.length - ok} 个` : ''}`, ok ? 'ok' : 'error')
      setSel(new Set()); load()
    } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }

  // 顶部状态卡：点了筛到对应的款；待审核直接去审核页
  const cards: { key: string; label: string; n: number; foot: string; on: boolean; tone?: string; go: () => void }[] = [
    { key: 'sampling', label: '拍样中', n: counts.sampling ?? 0, foot: counts.nophoto ? `${counts.nophoto} 款还没传照片` : '照片都传了', on: quick === 'nophoto', go: () => (counts.nophoto ? setQuick(quick === 'nophoto' ? '' : 'nophoto') : pickTab('sampling')) },
    { key: 'processing', label: '出图中', n: counts.processing, foot: '一键出图正在跑', on: quick === 'processing', go: () => setQuick(quick === 'processing' ? '' : 'processing') },
    ...(home && home.counts.review_pending !== null ? [{ key: 'review', label: '待审核', n: home.counts.review_pending, foot: '审核通过才能下载成品', on: false, go: () => nav('/review') }] : []),
    { key: 'failed', label: '出图失败', n: counts.failed, foot: counts.failed ? '需要重跑' : '没有失败的', on: quick === 'failed', tone: counts.failed ? 'danger' : '', go: () => setQuick(quick === 'failed' ? '' : 'failed') },
    { key: 'listed', label: '已上架', n: counts.listed ?? 0, foot: `${counts.pending ?? 0} 款待上架`, on: !quick && tab === 'listed', go: () => pickTab('listed') }
  ]

  return (
    <>
      <Topbar title="商品库" sub={<>{counts.all} 款在用<span className="sep" />{counts.pending ?? 0} 款待上架{home?.counts.review_pending ? <><span className="sep" />{home.counts.review_pending} 款等你审核</> : null}</>}>
        {can('product.edit') && <button className="btn btn-primary" onClick={() => setCreating(true)}><Plus />新建商品</button>}
      </Topbar>
      <div className="scroll"><main className="content">
        <div className="stage-cards">{cards.map(c => (
          <button key={c.key} className={'stage-card' + (c.on ? ' on' : '') + (c.tone ? ' ' + c.tone : '')} onClick={c.go}>
            <span className="stage-label">{c.label}</span>
            <span className="stage-n mono">{c.n}</span>
            <span className="stage-foot">{c.foot}</span>
          </button>
        ))}</div>
        <Tabs value={tab} onChange={pickTab} items={[
          { key: 'all', label: '全部', count: counts.all },
          { key: 'sampling', label: '拍样中', count: counts.sampling ?? 0 },
          { key: 'pending', label: '待上架', count: counts.pending ?? 0 },
          { key: 'listed', label: '已上架', count: counts.listed ?? 0 },
          { key: 'archived', label: '已归档', count: counts.archived ?? 0 }
        ]} />
        <div className="toolbar">
          {quick && <span className="tag">只看{QUICK_NAME[quick]}<button className="tag-x" onClick={() => setQuick('')} aria-label="取消筛选"><X /></button></span>}
          {(cats?.length || 0) > 1 && (
            <select className="input" style={{ width: 120 }} value={cat} onChange={e => setCat(e.target.value)}>
              <option value="">全部品类</option>
              {cats!.map(c => <option key={c.id} value={c.id}>{c.name}{c.disabled ? '（停用）' : ''}</option>)}
            </select>
          )}
          {shops.length > 1 && (
            <select className="input" style={{ width: 150 }} value={shop} onChange={e => setShop(e.target.value)}>
              <option value="">全部店铺</option>
              {shops.map(s => <option key={s.id} value={s.id}>{s.name}</option>)}
            </select>
          )}
          <div className="segmented"><button className={mine ? '' : 'active'} onClick={() => setMine(false)}>所有人</button><button className={mine ? 'active' : ''} onClick={() => setMine(true)}>我建的</button></div>
          <span className="spacer" />
          <label className="field" style={{ width: 220 }}><Search /><input placeholder="搜索款名" value={q} onChange={e => setQ(e.target.value)} /></label>
          <div className="segmented">
            <button className={view === 'table' ? 'active' : ''} onClick={() => pickView('table')}><Table2 />表格</button>
            <button className={view === 'grid' ? 'active' : ''} onClick={() => pickView('grid')}><LayoutGrid />图墙</button>
          </div>
        </div>
        <section className="panel">
          {picked.length > 0 && (
            <div className="bulkbar lib-bulk">
              <span className="count">已选 <span className="mono">{picked.length}</span> 款<span className="t3"> · 共 <span className="mono">{picked.reduce((a, p) => a + (p.asset_count || 0), 0)}</span> 张素材</span></span>
              <button className="btn btn-ghost btn-sm" onClick={() => setSel(new Set())}>取消选择</button>
              <div className="actions">
                {can('export.run') && <button className="btn btn-secondary btn-sm" disabled={busy} onClick={download}><Download />下载素材</button>}
                {can('render.run') && <button className="btn btn-primary btn-sm" disabled={busy} onClick={render}><Sparkles />批量出图 {picked.length} 款</button>}
              </div>
            </div>
          )}
          {!items ? <Loading /> : shown.length === 0 ? (
            <Empty title={q || shop || cat || mine || quick ? '没有符合条件的商品' : '还没有商品'} desc={can('product.edit') ? '点右上角「新建商品」，再上传实拍图。' : undefined} />
          ) : view === 'table' ? (
            <div className="table-wrap"><table className="table">
              <thead><tr>
                <th style={{ width: 36 }}><input type="checkbox" className="check" checked={allOn} onChange={() => setSel(allOn ? new Set() : new Set(shown.map(p => p.id)))} aria-label="全选" /></th>
                <th>商品</th><th>状态</th><th>店铺版本 · 6 步进度</th><th className="num">素材</th><th>负责人</th><th>更新</th>
              </tr></thead>
              <tbody>{shown.map(p => {
                const ps = productStatus[p.status]
                const who = owner(p)
                return (
                  <tr key={p.id} className={'clickable' + (sel.has(p.id) ? ' selected' : '')} onClick={() => nav(`/products/${p.id}`)}>
                    <td onClick={e => e.stopPropagation()}><input type="checkbox" className="check" checked={sel.has(p.id)} onChange={() => toggle(p.id)} aria-label="选择" /></td>
                    <td><div className="name-cell">
                      {p.cover_url ? <img className="cover" src={p.cover_url} alt="" /> : <span className="cover"><Package /></span>}
                      <div style={{ minWidth: 0 }}><div className="cell-title ellipsis">{p.name}</div><div className="cell-sub"><span className="mono">#{p.id}</span>{[p.category?.name, p.info?.style || p.info?.category].filter(Boolean).map(t => ` · ${t}`).join('')}</div></div>
                    </div></td>
                    <td><Pill kind={ps?.pill || 'pill-neutral'}>{ps?.label || p.status}</Pill></td>
                    <td><Lanes p={p} shopName={shopName} /></td>
                    <td className="num mono">{p.asset_count ? p.asset_count : <span className="t4">0</span>}</td>
                    <td>{who ? <span className="inline" style={{ gap: 6 }}><span className="avatar xs">{initials(who)}</span><span className="ellipsis">{who}</span></span> : <span className="t4">—</span>}</td>
                    <td className="t3">{fmtAgo(p.updated_at)}</td>
                  </tr>
                )
              })}</tbody>
            </table></div>
          ) : (
            <div className="lib-grid prod-grid">{shown.map(p => {
              const ps = productStatus[p.status]
              return (
                <div key={p.id} className={'lib-item' + (sel.has(p.id) ? ' selected' : '')} onClick={() => nav(`/products/${p.id}`)}>
                  <div className="prod-cover">
                    {p.cover_url ? <img src={p.cover_url.replace('w=240', 'w=480')} alt="" loading="lazy" /> : <Package />}
                    <input type="checkbox" className="check lib-check" checked={sel.has(p.id)} onClick={e => e.stopPropagation()} onChange={() => toggle(p.id)} aria-label="选择" />
                  </div>
                  <div className="prod-meta">
                    <div className="cell-title ellipsis">{p.name}</div>
                    <div className="inline" style={{ gap: 6, minWidth: 0 }}><Pill kind={ps?.pill || 'pill-neutral'}>{ps?.label || p.status}</Pill><span className="t3 fs-12 ellipsis">{p.asset_count || 0} 张 · {fmtAgo(p.updated_at)}</span></div>
                    <Lanes p={p} shopName={shopName} />
                  </div>
                </div>
              )
            })}</div>
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
  const { items: cats } = useCategories()
  const [catId, setCatId] = useState(0)
  const cat = cats?.find(c => c.id === catId) || cats?.[0]
  const [info, setInfo] = useState<Record<string, unknown>>({})
  const [cost, setCost] = useState('')
  const [supplier, setSupplier] = useState('')
  const [busy, setBusy] = useState(false)
  const submit = async () => {
    setBusy(true)
    try {
      const body: Record<string, unknown> = {
        name: name.trim(), shop_ids: shopIds, category_id: cat?.id, info
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
    <Drawer title="新建商品" width={500} onClose={onClose} foot={<>
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
      {cats && cats.length > 1 && (
        <div className="form-field">
          <span className="label req">品类</span>
          <div className="segmented">{cats.map(c => <button type="button" key={c.id} className={c.id === cat?.id ? 'active' : ''} onClick={() => { setCatId(c.id); setInfo({}) }}>{c.name}</button>)}</div>
          <span className="help">决定填哪些属性、按哪套拍摄编号分拣、文案按哪套规则写。</span>
        </div>
      )}
      {cat && <AttrFields attrs={cat.attrs} value={info} onChange={setInfo} />}
      {cat && missingAttrs(cat.attrs, info).length > 0 && <div className="t3 fs-12">带 * 的可以先空着，一键出图时 AI 会看照片补；补不了的会在合规检查里提醒。</div>}
      {can('product.cost') && <div className="form-row">
        <Field label="拿货成本（元）"><input className="input mono" inputMode="decimal" value={cost} onChange={e => setCost(e.target.value)} /></Field>
        <Field label="货源"><input className="input" value={supplier} onChange={e => setSupplier(e.target.value)} placeholder="诸暨 · 某某" /></Field>
      </div>}
    </Drawer>
  )
}
