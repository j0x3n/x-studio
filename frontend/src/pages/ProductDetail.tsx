import { Fragment, useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { Upload, Sparkles, Download, Plus, Trash2, ExternalLink, Package, Pencil, Shirt, Film, Play } from 'lucide-react'
import { api, qs, type Page } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import type { Asset, Job, Listing, Product, Task } from '../lib/types'
import { TaskMiniList, TaskDrawer, NewTaskDrawer } from '../components/TaskViews'
import { ProductStock, RelatedArticles } from '../components/ProductExtras'
import { Topbar, Pill, Loading, Empty, Tabs } from '../components/ui'
import { fmtAgo, money, productStatus, pipelineStatus, listingStatus, sourceName, fmtDuration } from '../lib/format'
import { shopLabel } from '../lib/shops'
import { ProductSteps } from '../components/ProductSteps'
import { useCategories, useCodeName, missingAttrs, emptyValue, attrText } from '../lib/categories'
import { AttrFields } from '../components/AttrFields'
import { useLightbox } from '../components/Lightbox'
import { CreativeTab } from '../components/CreativeTab'

type Detail = { product: Product; assets: Asset[] }
type ListingDetail = { listing: Listing; job: Job | null; outputs: { main34: string[]; main11: string[]; detail: string[]; white: string; zip: string } }
type TabKey = 'assets' | 'listings' | 'creative' | 'info' | 'stock' | 'tasks'

const STEP: Record<string, string> = { check: '检查照片', classify: 'AI 分拣', enhance: '补图和高清化', retouch: '修图', copy: '写文案', render: '出图' }

export function ProductDetail() {
  const { id } = useParams()
  const nav = useNavigate()
  const { me, can } = useAuth()
  const toast = useToast()
  const [d, setD] = useState<Detail | null>(null)
  const [tab, setTab] = useState<TabKey>('assets')
  const [tasks, setTasks] = useState<Task[] | null>(null)
  const canTasks = can('task.view')
  // 挂在这款商品上的任务（后端只按 product 对象查；店铺版本上的任务 target 也指向这款商品时一并显示）
  const loadTasks = useCallback(() => {
    if (!canTasks || !id) return
    api<Page<Task>>('/api/tasks' + qs({ view: 'all', status: '', target_type: 'product', target_id: id, page_size: 50 }))
      .then(r => setTasks(r.items || [])).catch(() => setTasks([]))
  }, [canTasks, id])
  useEffect(() => { loadTasks() }, [loadTasks])
  // 助手建了任务后刷新计数
  useEffect(() => { window.addEventListener('xs:tasks-changed', loadTasks); return () => window.removeEventListener('xs:tasks-changed', loadTasks) }, [loadTasks])
  const load = useCallback(() => api<Detail>(`/api/products/${id}`).then(setD).catch(e => { toast(errText(e), 'error'); nav('/products') }), [id, nav, toast])
  useEffect(() => { load() }, [load])
  if (!d) return <><Topbar crumbs={['商品', '…']} /><Loading /></>
  const p = d.product
  const ps = productStatus[p.status]
  const photos = d.assets.filter(a => a.kind === 'photo')
  const myShops = me?.shops || []
  const missingShops = myShops.filter(s => !p.listings.some(l => l.shop_id === s.id))

  const setStatus = async (status: string) => {
    try { await api(`/api/products/${p.id}`, { method: 'PATCH', body: { status } }); load() } catch (e) { toast(errText(e), 'error') }
  }
  const remove = async () => {
    if (!confirm(`删除「${p.name}」？素材和各店的成品图会一起删掉，不能恢复。`)) return
    try { await api(`/api/products/${p.id}`, { method: 'DELETE' }); toast('已删除', 'ok'); nav('/products') } catch (e) { toast(errText(e), 'error') }
  }
  const addListing = async (shopId: number) => {
    try { await api(`/api/products/${p.id}/listings`, { body: { shop_id: shopId } }); load() } catch (e) { toast(errText(e), 'error') }
  }

  return (
    <>
      <Topbar crumbs={[<a onClick={() => nav('/products')} className="link">商品库</a>]} title={<span className="inline">{p.name}<Pill kind={ps?.pill || 'pill-neutral'}>{ps?.label}</Pill></span>} sub={<><span className="mono">#{p.id}</span>{p.category && <><span className="sep" />{p.category.name}</>}<span className="sep" />{photos.length} 个照片和视频<span className="sep" />{p.listings.length} 个店铺版本<span className="sep" />更新于 {fmtAgo(p.updated_at)}</>}>
        {can('product.edit') && (
          <select className="input" style={{ width: 120 }} value={p.status} onChange={e => setStatus(e.target.value)}>
            {Object.entries(productStatus).map(([k, v]) => <option key={k} value={k}>{v.label}</option>)}
          </select>
        )}
        {(can('product.delete_any') || (can('product.delete_own') && p.created_by === me?.user.id)) &&
          <button className="btn btn-secondary" onClick={remove}><Trash2 />删除</button>}
      </Topbar>
      <div className="scroll"><main className="content">
        <ProductSteps product={p} photos={photos} onTab={setTab} />
        <Tabs value={tab} onChange={setTab} items={[
          { key: 'assets', label: '照片和视频', count: photos.length },
          ...(can('creative.run') && me?.role.scope !== 'shares' ? [{ key: 'creative' as const, label: 'AI 出图' }] : []),
          { key: 'listings', label: '店铺版本', count: p.listings.length },
          { key: 'info', label: '基础信息' },
          ...(can('inventory.view') ? [{ key: 'stock' as const, label: '库存' }] : []),
          ...(canTasks ? [{ key: 'tasks' as const, label: '任务', count: tasks?.filter(t => t.status !== 'done' && t.status !== 'canceled').length }] : [])
        ]} />
        {tab === 'assets' && <AssetsTab product={p} assets={photos} onChange={load} />}
        {tab === 'listings' && (
          <div className="stack">
            {p.listings.length === 0 && <section className="panel"><Empty title="还没有店铺版本" desc="加一个店铺版本，才能写文案和出图。" /></section>}
            {p.listings.map(l => <ListingCard key={l.id} listing={l} shopName={myShops.find(s => s.id === l.shop_id)?.name || `店铺 ${l.shop_id}`} onChange={load} />)}
            {can('product.edit') && missingShops.length > 0 && (
              <div className="toolbar">{missingShops.map(s => (
                <button key={s.id} className="btn btn-secondary" onClick={() => addListing(s.id)}><Plus />加到{s.name}<span className="t4">{shopLabel(s)}</span></button>
              ))}</div>
            )}
          </div>
        )}
        {tab === 'creative' && <CreativeTab product={p} videos={photos.filter(a => a.media === 'video')} />}
        {tab === 'info' && <InfoTab product={p} onChange={load} />}
        {tab === 'stock' && <ProductStock product={p} />}
        {tab === 'tasks' && <TasksTab product={p} tasks={tasks} onChange={loadTasks} />}
      </main></div>
    </>
  )
}

function AssetsTab({ product, assets, onChange }: { product: Product; assets: Asset[]; onChange: () => void }) {
  const lightbox = useLightbox()
  const nav = useNavigate()
  const { me, can } = useAuth()
  const toast = useToast()
  const input = useRef<HTMLInputElement>(null)
  const [over, setOver] = useState(false)
  const [busy, setBusy] = useState(false)
  const codeName = useCodeName()
  const upload = async (files: FileList | File[]) => {
    const list = Array.from(files).filter(f => /image\/(jpeg|png|webp)|video\/(mp4|quicktime|webm)/.test(f.type))
    if (!list.length) return toast('只认 jpg、png、webp 图片和 mp4、mov、webm 视频', 'error')
    if (list.some(f => f.type.startsWith('video/') && f.size > 500 * 1024 * 1024)) return toast('视频不能超过 500 MB', 'error')
    const form = new FormData()
    list.forEach(f => form.append('files', f))
    setBusy(true)
    try { await api(`/api/products/${product.id}/assets`, { form }); toast(`已上传 ${list.length} 个`, 'ok'); onChange() }
    catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const box = (a: Asset) => a.media === 'video'
    ? { src: a.url, video: true, poster: a.poster_url, title: a.title || a.orig_name, meta: fmtDuration(a.duration_ms), download: '' }
    : { src: a.url, title: a.title || a.orig_name, meta: `${a.width}×${a.height}`, download: '' }
  return (
    <section className="panel">
      <div className="panel-head"><div className="titles"><div className="panel-title">照片和视频</div><div className="panel-sub">传实拍原图和视频；照片会自动认出是哪一类（平铺、微距、上身……），出图时按类别取用</div></div></div>
      <div className="panel-pad stack">
        {can('asset.upload') && (
          <div className={'drop' + (over ? ' over' : '')} onClick={() => input.current?.click()}
            onDragOver={e => { e.preventDefault(); setOver(true) }} onDragLeave={() => setOver(false)}
            onDrop={e => { e.preventDefault(); setOver(false); upload(e.dataTransfer.files) }}>
            <Upload /><b>{busy ? '上传中…' : '点击或拖入照片、视频'}</b>照片 jpg / png / webp 不超过 25 MB；视频 mp4 / mov / webm 不超过 500 MB；可以一次选多个
            <input ref={input} type="file" accept="image/jpeg,image/png,image/webp,video/mp4,video/quicktime,video/webm" multiple hidden onChange={e => e.target.files && upload(e.target.files)} />
          </div>
        )}
        {assets.length === 0 ? <Empty title="还没有照片和视频" /> : (
          <div className="thumbs">{assets.map((a, ai) => {
            const src = sourceName[a.source_override || a.source] || sourceName.original
            return (
              <div key={a.id} className="thumb product-tryon-thumb">
                <button type="button" className="zoomable" title={a.ai_note || a.orig_name} onClick={() => lightbox(assets.map(box), ai)}>
                  {a.media === 'video'
                    ? (a.poster_url ? <img src={a.poster_url} alt={a.orig_name} loading="lazy" /> : <span className="lib-video-blank"><Film /></span>)
                    : <img src={a.url + '?w=240'} alt={a.orig_name} loading="lazy" />}
                  {a.media === 'video' && <span className="lib-video-badge mono"><Play />{fmtDuration(a.duration_ms) || '视频'}</span>}
                </button>
                {src.label !== '原图' && <span className="src"><Pill kind={src.pill} dot={false}>{src.label}</Pill></span>}
                <div className="meta">
                  {a.media === 'video' ? <span className="code">视频</span>
                    : <span className={'code' + (a.code ? '' : ' none')} title={a.code ? `拍摄编号 ${a.code}` : '还没认出是哪一类照片，一键出图时会自动分拣'}>{a.code ? codeName(a.code, product.category_id) : '未分类'}</span>}
                  <span className="grow" />{a.width > 0 && <span className="t4 mono">{a.width}×{a.height}</span>}
                </div>
                {a.kind === 'photo' && a.media !== 'video' && can('model.view') && can('asset.process') && can('asset.upload') && me?.role.scope !== 'shares' && <button className="btn btn-ghost btn-sm" onClick={() => nav('/tryon?assets=' + a.id)}><Shirt />试穿试戴</button>}
              </div>
            )
          })}</div>
        )}
      </div>
    </section>
  )
}

function ListingCard({ listing, shopName, onChange }: { listing: Listing; shopName: string; onChange: () => void }) {
  const { can } = useAuth()
  const toast = useToast()
  const lightbox = useLightbox()
  const [d, setD] = useState<ListingDetail | null>(null)
  const load = useCallback(() => api<ListingDetail>(`/api/listings/${listing.id}`).then(setD).catch(() => {}), [listing.id])
  useEffect(() => { load() }, [load])
  // 任务在跑时每 1.5 秒刷新进度
  const running = d?.job && (d.job.status === 'queued' || d.job.status === 'running')
  useEffect(() => {
    if (!running) return
    const t = setInterval(load, 1500)
    return () => clearInterval(t)
  }, [running, load])
  // 任务跑完后刷新整页（素材里会多出 AI 补的图）
  const status = d?.job?.status
  const prev = useRef(status)
  useEffect(() => { if (prev.current && prev.current !== 'done' && status === 'done') onChange(); prev.current = status }, [status, onChange])

  const run = async () => {
    try { await api(`/api/listings/${listing.id}/run`, { method: 'POST' }); toast('已开始一键出图', 'ok'); load() } catch (e) { toast(errText(e), 'error') }
  }
  const l = d?.listing || listing
  const st = pipelineStatus[l.pipeline_status] || pipelineStatus.draft
  const ls = listingStatus[l.status]
  const out = d?.outputs
  const title = (l.meta as { title?: string } | undefined)?.title
  return (
    <section className="panel">
      <div className="panel-head">
        <span className="icon-tile"><Package /></span>
        <div className="titles">
          <div className="panel-title inline">{shopName}
            <Pill kind={ls?.pill || 'pill-neutral'}>{ls?.label}</Pill>
            <Pill kind={st.pill}>{st.label === '失败' ? '出图失败' : st.label}</Pill>
          </div>
          <div className="panel-sub ellipsis">{title || '还没有标题'}</div>
        </div>
        {l.url && <a className="btn btn-ghost btn-sm" href={l.url} target="_blank" rel="noreferrer"><ExternalLink />商品链接</a>}
        {out?.zip && can('export.run') && <a className="btn btn-secondary btn-sm" href={out.zip}><Download />下载成品</a>}
        {can('render.run') && <button className="btn btn-primary btn-sm" disabled={!!running} onClick={run}><Sparkles />{running ? '出图中…' : out?.zip ? '重新出图' : '一键出图'}</button>}
      </div>
      <div className="panel-pad stack" style={{ gap: 12 }}>
        {running && d?.job && (
          <div className="stack" style={{ gap: 6 }}>
            <div className="row between fs-12"><span>{STEP[l.pipeline_step] || '排队中'} · {d.job.message}</span><span className="mono">{d.job.progress}%</span></div>
            <div className="progress"><span style={{ width: `${d.job.progress}%` }} /></div>
          </div>
        )}
        {l.pipeline_status === 'failed' && <div className="status-note">出图失败：{l.pipeline_error || d?.job?.message}</div>}
        {out && (out.main34.length > 0 || out.detail.length > 0) ? (
          <div className="stack" style={{ gap: 8 }}>
            <div className="fs-12 t3">主图 3:4 · {out.main34.length} 张　详情页 · {out.detail.length} 张{out.white ? '　白底图 · 1 张' : ''}</div>
            <div className="thumbs">
              {[...out.main34, ...(out.white ? [out.white] : [])].map((u, n, all) => (
                <button key={u} type="button" className="thumb" onClick={() => lightbox(all.map((x, k) => ({ src: x, title: out.white === x ? '白底图' : `主图 3:4 · 第 ${k + 1} 张`, download: '' })), n)}><img src={u + '?w=240'} alt="" loading="lazy" /></button>
              ))}
            </div>
          </div>
        ) : !running && <div className="t3 fs-12">还没有成品。传好原图后点「一键出图」，一款大约 1–3 分钟。</div>}
        {d?.listing.meta && Array.isArray((d.listing.meta as { compliance?: unknown[] }).compliance) && ((d.listing.meta as { compliance: { level: string; text: string }[] }).compliance.length > 0) && (
          <div className="stack" style={{ gap: 4 }}>
            <div className="fs-12 t2" style={{ fontWeight: 600 }}>合规检查</div>
            {(d.listing.meta as { compliance: { level: string; text: string }[] }).compliance.map((c, i) => (
              <div key={i} className="fs-12"><Pill kind={c.level === 'error' ? 'pill-danger' : 'pill-warning'} dot={false}>{c.level === 'error' ? '必须改' : '看一眼'}</Pill> {c.text}</div>
            ))}
          </div>
        )}
      </div>
    </section>
  )
}

// 老商品里不在品类模板中的字段（如 notes、旧的 category）照样显示
const LEGACY_LABEL: Record<string, string> = { series: '系列', category: '款式', style: '款式', pearl: '珍珠', shape: '形状', size: '尺寸', metal: '配件', colors: '颜色', price: '售价', highlights: '卖点', notes: '备注' }

function InfoTab({ product, onChange }: { product: Product; onChange: () => void }) {
  const { can } = useAuth()
  const toast = useToast()
  const { items: cats } = useCategories(true)
  const [editing, setEditing] = useState(false)
  const [catId, setCatId] = useState(product.category_id || 0)
  const [draft, setDraft] = useState<Record<string, unknown>>({})
  const [busy, setBusy] = useState(false)
  const info = product.info || {}
  const cat = cats?.find(c => c.id === product.category_id)
  const attrs = cat?.attrs || []
  const known = new Set(attrs.map(a => a.key))
  const rows: [string, string, unknown][] = [
    ...attrs.filter(a => !emptyValue(info[a.key])).map(a => [a.key, a.label, info[a.key]] as [string, string, unknown]),
    ...Object.entries(info).filter(([k, v]) => !known.has(k) && LEGACY_LABEL[k] && !emptyValue(v)).map(([k, v]) => [k, LEGACY_LABEL[k], v] as [string, string, unknown])
  ]
  const missing = missingAttrs(attrs, info)
  const editCat = cats?.find(c => c.id === catId) || cat
  const start = () => { setCatId(product.category_id || 0); setDraft({ ...info }); setEditing(true) }
  const save = async () => {
    setBusy(true)
    try {
      const body: Record<string, unknown> = { info: draft }
      if (catId && catId !== product.category_id) body.category_id = catId
      await api(`/api/products/${product.id}`, { method: 'PATCH', body })
      toast('已保存', 'ok'); setEditing(false); onChange()
    } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const enabled = (cats || []).filter(c => !c.disabled || c.id === product.category_id)
  return (
    <div className="two-col">
      <section className="panel">
        <div className="panel-head">
          <div className="titles"><div className="panel-title">基础信息</div>{cat && <div className="panel-sub">品类：{cat.name}{cat.disabled ? '（已停用）' : ''}</div>}</div>
          <span className="grow" />
          {can('product.edit') && !editing && <button className="btn btn-secondary btn-sm" onClick={start}><Pencil />编辑</button>}
        </div>
        {editing ? (
          <div className="panel-pad stack">
            {enabled.length > 1 && (
              <div className="form-field">
                <span className="label">品类</span>
                <div className="segmented">{enabled.map(c => <button type="button" key={c.id} className={c.id === catId ? 'active' : ''} onClick={() => setCatId(c.id)}>{c.name}</button>)}</div>
                {catId !== product.category_id && <span className="help">改品类后，以后分拣和出图按新品类的拍摄编号和槽位来，已分拣的素材编号不变。</span>}
              </div>
            )}
            {editCat && <AttrFields attrs={editCat.attrs} value={draft} onChange={setDraft} />}
            <div className="toolbar" style={{ justifyContent: 'flex-end', margin: 0 }}>
              <button className="btn btn-secondary" onClick={() => setEditing(false)}>取消</button>
              <button className="btn btn-primary" disabled={busy} onClick={save}>保存</button>
            </div>
          </div>
        ) : (
          <div className="panel-pad stack">
            {rows.length === 0 ? <div className="t3">还没填，一键出图时 AI 会看照片补全。</div> : (
              <dl className="kv">{rows.map(([k, label, v]) => <Fragment key={k}><dt>{label}</dt><dd className={k === 'price' ? 'mono' : ''}>{k === 'price' ? money(v) : attrText(attrs.find(a => a.key === k), v)}</dd></Fragment>)}</dl>
            )}
            {missing.length > 0 && <div className="fs-12 t3">还没填：{missing.join('、')}</div>}
          </div>
        )}
      </section>
      {(product.cost || product.supplier) && (
        <section className="panel">
          <div className="panel-head"><div className="panel-title">成本和货源</div><span className="grow" /><span className="t4 fs-12">只有有权限的人看得到</span></div>
          <div className="panel-pad"><dl className="kv">
            <dt>拿货成本</dt><dd className="mono">{money(product.cost?.unit)}</dd>
            <dt>货源</dt><dd>{String(product.supplier?.name || '—')}</dd>
            {product.supplier?.link ? <><dt>链接</dt><dd><a className="link" href={String(product.supplier.link)} target="_blank" rel="noreferrer">{String(product.supplier.link)}</a></dd></> : null}
          </dl></div>
        </section>
      )}
      {can('kb.view') && <RelatedArticles product={product} />}
    </div>
  )
}

function TasksTab({ product, tasks, onChange }: { product: Product; tasks: Task[] | null; onChange: () => void }) {
  const [open, setOpen] = useState<number | null>(null)
  const [creating, setCreating] = useState(false)
  const [showDone, setShowDone] = useState(false)
  const list = (tasks || []).filter(t => showDone || (t.status !== 'done' && t.status !== 'canceled'))
  const closed = (tasks || []).length - (tasks || []).filter(t => t.status !== 'done' && t.status !== 'canceled').length
  return (
    <section className="panel">
      <div className="panel-head">
        <div className="titles"><div className="panel-title">相关任务</div><div className="panel-sub">挂在这款商品上的任务，包括自动派活建的</div></div>
        {closed > 0 && <label className="row t3 fs-12"><input type="checkbox" className="check" checked={showDone} onChange={e => setShowDone(e.target.checked)} />显示已完成 {closed} 件</label>}
        <button className="btn btn-secondary btn-sm" onClick={() => setCreating(true)}><Plus />新建任务</button>
      </div>
      {!tasks ? <Loading /> : <TaskMiniList items={list} onOpen={t => setOpen(t.id)} empty={<Empty title="没有进行中的任务" desc="拍样、修图、处理出图失败等事情可以建成任务派给同事。" />} />}
      {open && <TaskDrawer id={open} onClose={() => setOpen(null)} onChanged={onChange} />}
      {creating && <NewTaskDrawer target={{ type: 'product', id: product.id, label: product.name }} onClose={() => setCreating(false)} onCreated={t => { setCreating(false); onChange(); setOpen(t.id) }} />}
    </section>
  )
}
