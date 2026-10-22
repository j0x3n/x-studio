import { Fragment, useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { LayoutTemplate, Star, Ban, RotateCcw, Sparkles, Check, ExternalLink, Layers } from 'lucide-react'
import { api, qs } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { useListingRefs } from '../lib/listings'
import { useCategories } from '../lib/categories'
import type { DetailTemplate, RenderState } from '../lib/types'
import { Topbar, Pill, Loading, Empty, Drawer } from '../components/ui'
import { CreativeTemplates } from '../components/CreativeTemplates'
import { CreativePlans } from '../components/CreativePlans'

type Templates = { detail: DetailTemplate[]; main: { key: string; name: string; file: string }[] }

// 模板库：生图模板（AI 出图用，默认）和旧的 HTML 详情页模板（停用但保留）
// 第三个分页是出图方案（?mode=plans 直接打开）
export function TemplateCenter() {
  const { can } = useAuth()
  const [sp, setSp] = useSearchParams()
  const ai = can('creative.run')
  const mode: 'ai' | 'html' | 'plans' = sp.get('mode') === 'plans' && ai ? 'plans' : sp.get('mode') === 'html' || !ai ? 'html' : 'ai'
  const setMode = (m: string) => setSp(m === 'ai' ? {} : { mode: m }, { replace: true })
  const switcher = ai ? <div className="segmented"><button className={mode === 'ai' ? 'active' : ''} onClick={() => setMode('ai')}>生图模板</button><button className={mode === 'plans' ? 'active' : ''} onClick={() => setMode('plans')}>出图方案</button><button className={mode === 'html' ? 'active' : ''} onClick={() => setMode('html')}>HTML 模板（旧）</button></div> : null
  return mode === 'ai' ? <CreativeTemplates switcher={switcher} /> : mode === 'plans' ? <CreativePlans switcher={switcher} /> : <HtmlTemplates switcher={switcher} />
}

function HtmlTemplates({ switcher }: { switcher: ReactNode }) {
  const { me, can } = useAuth()
  const toast = useToast()
  const nav = useNavigate()
  const shops = me?.shops || []
  const [shopId, setShopId] = useState(shops[0]?.id || 0)
  const [data, setData] = useState<Templates | null>(null)
  const [sel, setSel] = useState('')
  const [listingId, setListingId] = useState(0)
  const [state, setState] = useState<RenderState | null>(null)
  const [zoom, setZoom] = useState(0.5)
  const [batch, setBatch] = useState(false)
  const [catKey, setCatKey] = useState('')
  const { items: cats } = useCategories()
  const refs = useListingRefs()
  // 选了品类时，模板和预览款都只看这个品类的
  const shopRefs = (refs || []).filter(r => r.shop_id === shopId && (!catKey || r.product.category?.key === catKey))
  const manage = can('template.manage') && !!shopId
  const runner = can('render.run')

  const load = useCallback(() => {
    api<Templates>('/api/templates' + qs({ shop_id: shopId || undefined, category: catKey || undefined })).then(d => {
      setData(d)
      setSel(s => (d.detail.some(t => t.key === s) ? s : (d.detail.find(t => t.is_default) || d.detail[0])?.key || ''))
    }).catch(e => toast(errText(e), 'error'))
  }, [shopId, catKey, toast])
  useEffect(() => { load() }, [load])
  // 换店铺时预览款换成这个店的第一款
  useEffect(() => { setListingId(id => (shopRefs.some(r => r.id === id) ? id : shopRefs[0]?.id || 0)) }, [shopId, catKey, refs]) // eslint-disable-line react-hooks/exhaustive-deps
  const loadState = useCallback(() => {
    if (!listingId) { setState(null); return }
    api<RenderState>(`/api/listings/${listingId}/render`).then(setState).catch(() => setState(null))
  }, [listingId])
  useEffect(() => { loadState() }, [loadState])

  const t = data?.detail.find(x => x.key === sel)
  const nameOf = (key: string) => data?.detail.find(x => x.key === key)?.name || key

  const savePrefs = async (default_detail: string, disabled: string[]) => {
    try { await api(`/api/shops/${shopId}/template-prefs`, { method: 'PUT', body: { default_detail, disabled } }); toast('已保存', 'ok'); load() } catch (e) { toast(errText(e), 'error') }
  }
  const curDefault = data?.detail.find(x => x.is_default)?.key || 'navy'
  const curDisabled = data?.detail.filter(x => x.disabled).map(x => x.key) || []
  const useForListing = async (rerun: boolean) => {
    if (!t || !listingId) return
    try {
      await api(`/api/listings/${listingId}/render`, { method: 'PUT', body: { detail_template: t.key, mode: 'html' } })
      if (rerun) { await api(`/api/listings/${listingId}/render/run`, { method: 'POST' }); toast('已换模板，开始重新出图', 'ok') }
      else toast('已换模板，下次出图生效', 'ok')
      loadState()
    } catch (e) { toast(errText(e), 'error') }
  }

  const effective = state?.settings.effective_detail_template
  return (
    <>
      <Topbar crumbs={['模板库', ...(t ? [t.name] : [])]}>{switcher}</Topbar>
      <div className="lib">
        <aside className="sidebar lib-filters">
          {shops.length > 0 && (
            <div className="nav-group">
              <div className="nav-label">店铺</div>
              <select className="input" value={shopId} onChange={e => setShopId(Number(e.target.value))}>
                {shops.map(s => <option key={s.id} value={s.id}>{s.name}</option>)}
              </select>
            </div>
          )}
          {(cats?.length || 0) > 1 && (
            <div className="nav-group">
              <div className="nav-label">品类</div>
              <select className="input" value={catKey} onChange={e => setCatKey(e.target.value)}>
                <option value="">全部品类</option>
                {cats!.map(c => <option key={c.id} value={c.key}>{c.name}</option>)}
              </select>
            </div>
          )}
          <div className="nav-group">
            <div className="nav-label">详情页模板<span className="end">{data?.detail.length ?? ''}</span></div>
            {!data ? <div className="t3 fs-12" style={{ padding: 8 }}>加载中…</div> : data.detail.map(x => (
              <a key={x.key} className={'nav-item tpl-item' + (x.key === sel ? ' active' : '') + (x.disabled ? ' off' : '')} onClick={() => setSel(x.key)}>
                <LayoutTemplate />
                <span className="ellipsis">{x.name}</span>
                <span className="end inline">
                  {x.is_default && <span className="tag tag-blue">默认</span>}
                  {x.disabled && <span className="tag tag-gray">停用</span>}
                  {listingId && effective === x.key && <span className="tag tag-green">这款在用</span>}
                </span>
              </a>
            ))}
          </div>
          {data && data.main.length > 0 && (
            <div className="nav-group">
              <div className="nav-label">主图模板</div>
              {data.main.map(m => <div key={m.key} className="nav-item" style={{ cursor: 'default' }}><Layers />{m.name}<span className="end t4 fs-12">5 种版式</span></div>)}
            </div>
          )}
        </aside>

        <div className="lib-main">
          <div className="lib-toolbar">
            <span className="t3 fs-12">用哪款预览</span>
            <select className="input" style={{ width: 200 }} value={listingId} onChange={e => setListingId(Number(e.target.value))}>
              {shopRefs.length === 0 && <option value={0}>这个店还没有款</option>}
              {shopRefs.map(r => <option key={r.id} value={r.id}>{r.product.name}</option>)}
            </select>
            {state && effective && <span className="t4 fs-12">{state.settings.mode === 'ai' ? '这款一键出图用 AI 出图，HTML 模板不生效；在下面换成某套模板会改回 HTML' : <>这款现在用「{nameOf(effective)}」{state.settings.detail_template ? '' : '（店铺默认）'}</>}</span>}
            <span className="spacer" />
            <div className="segmented">{[[0.35, '小'], [0.5, '中'], [0.75, '大']].map(([z, l]) => <button key={z} className={zoom === z ? 'active' : ''} onClick={() => setZoom(z as number)}>{l}</button>)}</div>
            {t && listingId > 0 && <a className="btn btn-ghost btn-sm" href={`/render/详情页模板/${t.file}?p=${listingId}`} target="_blank" rel="noreferrer"><ExternalLink />新窗口</a>}
          </div>
          <div className="scroll tpl-stage">
            {!data ? <Loading /> : !t ? <Empty title="没有可用的模板" /> : listingId ? (
              <ScaledFrame key={`${t.file}-${listingId}`} src={`/render/详情页模板/${t.file}?p=${listingId}`} zoom={zoom} />
            ) : t.thumb_url ? <img className="tpl-thumb" src={t.thumb_url} alt="" style={{ width: 750 * zoom }} /> : (
              <Empty title="选一款来预览" desc="模板用真实文案和照片渲染。先在商品里建店铺版本，跑过一键出图后预览最准。" />
            )}
          </div>
        </div>

        <aside className="inspector lib-inspector">
          {t && <>
            <div className="inspector-body">
              <div className="inspector-section">
                <div className="head"><span className="section-title">{t.name}</span><span className="mono t4 fs-12">{t.key}</span></div>
                <div className="inline">
                  {t.is_default && <Pill kind="pill-info">店铺默认</Pill>}
                  {t.disabled ? <Pill kind="pill-neutral">已停用</Pill> : <Pill kind="pill-success">可用</Pill>}
                </div>
                <div className="fs-12 t2">{t.fit}</div>
                <div className="fs-12 t3">适用品类：{t.categories?.length ? t.categories.map(k => cats?.find(c => c.key === k)?.name || k).join('、') : '全部'}</div>
              </div>
              {Object.keys(t.optional || {}).length > 0 && (
                <div className="inspector-section">
                  <div className="head"><span className="sub-title">这套模板多出来的字段</span></div>
                  <dl className="kv narrow tpl-kv">{Object.entries(t.optional).map(([k, v]) => <Fragment key={k}><dt className="mono">{k}</dt><dd className="fs-12">{v}</dd></Fragment>)}</dl>
                  <span className="fs-12 t4">其他模板会忽略这些字段；AI 写文案时按模板需要补。</span>
                </div>
              )}
              {listingId > 0 && runner && (
                <div className="inspector-section">
                  <div className="head"><span className="sub-title">给预览的这款用</span></div>
                  {effective === t.key && state?.settings.mode !== 'ai' ? <span className="fs-12 t3 inline"><Check size={14} />这款已经在用这套模板</span> : <>
                    <button className="btn btn-primary btn-block" disabled={t.disabled} onClick={() => useForListing(true)}><Sparkles />换成这套并重新出图</button>
                    <button className="btn btn-secondary btn-block" disabled={t.disabled} onClick={() => useForListing(false)}>只换模板，先不出图</button>
                  </>}
                  {shopRefs.find(r => r.id === listingId) && <a className="link fs-12" onClick={() => nav(`/products/${shopRefs.find(r => r.id === listingId)!.product.id}`)}>去商品页看成品</a>}
                </div>
              )}
              {runner && shopId > 0 && (
                <div className="inspector-section">
                  <div className="head"><span className="sub-title">批量出图</span></div>
                  <button className="btn btn-secondary btn-block" disabled={t.disabled} onClick={() => setBatch(true)}><Layers />挑几款用这套出图</button>
                </div>
              )}
            </div>
            {manage && (
              <div className="inspector-foot stack" style={{ gap: 6 }}>
                <span className="fs-12 t3">店铺设置 · {shops.find(s => s.id === shopId)?.name}</span>
                <button className="btn btn-secondary btn-block" disabled={t.is_default || t.disabled} onClick={() => savePrefs(t.key, curDisabled)}><Star />{t.is_default ? '已是默认模板' : '设为默认模板'}</button>
                {t.disabled
                  ? <button className="btn btn-secondary btn-block" onClick={() => savePrefs(curDefault, curDisabled.filter(k => k !== t.key))}><RotateCcw />重新启用</button>
                  : <button className="btn btn-ghost btn-block" disabled={t.is_default} title={t.is_default ? '默认模板不能停用，先把别的设为默认' : undefined} onClick={() => savePrefs(curDefault, [...curDisabled, t.key])}><Ban />这个店不用这套</button>}
              </div>
            )}
          </>}
        </aside>
      </div>
      {batch && t && <BatchDrawer tpl={t} shopId={shopId} onClose={() => setBatch(false)} />}
    </>
  )
}

// 模板页面宽 750，按比例缩小放进来；同源页面，读出实际高度
function ScaledFrame({ src, zoom }: { src: string; zoom: number }) {
  const ref = useRef<HTMLIFrameElement>(null)
  const [h, setH] = useState(6000)
  const [ready, setReady] = useState(false)
  const onLoad = () => {
    setReady(true)
    const doc = ref.current?.contentDocument
    if (!doc?.body) return
    const measure = () => setH(Math.max(doc.documentElement.scrollHeight, doc.body.scrollHeight))
    measure()
    const ro = new ResizeObserver(measure)
    ro.observe(doc.body)
  }
  return (
    <div className="tpl-frame" style={{ width: 750 * zoom, height: h * zoom }}>
      {!ready && <div className="loading">渲染中…</div>}
      <iframe ref={ref} src={src} title="模板预览" onLoad={onLoad} style={{ width: 750, height: h, transform: `scale(${zoom})` }} />
    </div>
  )
}

function BatchDrawer({ tpl, shopId, onClose }: { tpl: DetailTemplate; shopId: number; onClose: () => void }) {
  const toast = useToast()
  const refs = useListingRefs()
  const list = (refs || []).filter(r => r.shop_id === shopId)
  const [ids, setIds] = useState<number[]>([])
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<{ listing_id: number; job_id?: number; skipped_reason?: string }[] | null>(null)
  const run = async () => {
    setBusy(true)
    try {
      const r = await api<{ items: { listing_id: number; job_id?: number; skipped_reason?: string }[] }>('/api/render/batch', { body: { listing_ids: ids, detail_template: tpl.key } })
      setResult(r.items)
      const ok = r.items.filter(i => i.job_id).length
      toast(`已排队 ${ok} 款${ok < r.items.length ? `，跳过 ${r.items.length - ok} 款` : ''}`, ok ? 'ok' : 'error')
    } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const name = (id: number) => list.find(r => r.id === id)?.product.name || `#${id}`
  return (
    <Drawer title={`用「${tpl.name}」批量出图`} width={440} onClose={onClose} foot={result ? <button className="btn btn-primary" onClick={onClose}>好的</button> : <>
      <button className="btn btn-secondary" onClick={onClose}>取消</button>
      <button className="btn btn-primary" disabled={busy || !ids.length} onClick={run}>出图 {ids.length || ''} 款</button>
    </>}>
      {result ? (
        <div className="list">{result.map(r => (
          <div key={r.listing_id} className="list-item"><span className="title">{name(r.listing_id)}</span><span className="end">{r.job_id ? <Pill kind="pill-success">已排队</Pill> : <span className="fs-12 t3">跳过：{r.skipped_reason}</span>}</span></div>
        ))}</div>
      ) : !refs ? <Loading /> : list.length === 0 ? <Empty title="这个店还没有款" /> : <>
        <div className="row between fs-12 t3">
          <span>选中的款会改用这套模板，只重跑出图这一步，一次最多 50 款。</span>
        </div>
        <label className="filter-opt"><input type="checkbox" className="check" checked={ids.length === list.length} onChange={e => setIds(e.target.checked ? list.slice(0, 50).map(r => r.id) : [])} />全选</label>
        <div className="checks col">{list.map(r => (
          <label key={r.id}><input type="checkbox" className="check" checked={ids.includes(r.id)} onChange={e => setIds(v => (e.target.checked ? (v.length < 50 ? [...v, r.id] : v) : v.filter(x => x !== r.id)))} />{r.product.name}</label>
        ))}</div>
      </>}
    </Drawer>
  )
}
