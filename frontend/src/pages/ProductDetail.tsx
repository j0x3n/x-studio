import { Fragment, useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { ArrowLeft, Upload, Sparkles, Download, Plus, Trash2, ExternalLink, Package } from 'lucide-react'
import { api } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import type { Asset, Job, Listing, Product } from '../lib/types'
import { Topbar, PageHead, Pill, Loading, Empty, Tabs } from '../components/ui'
import { fmtAgo, money, productStatus, pipelineStatus, listingStatus, sourceName, platformName } from '../lib/format'

type Detail = { product: Product; assets: Asset[] }
type ListingDetail = { listing: Listing; job: Job | null; outputs: { main34: string[]; main11: string[]; detail: string[]; white: string; zip: string } }
type TabKey = 'assets' | 'listings' | 'info'

const STEP: Record<string, string> = { check: '检查照片', classify: 'AI 分拣', enhance: '补图和高清化', retouch: '修图', copy: '写文案', render: '出图' }

export function ProductDetail() {
  const { id } = useParams()
  const nav = useNavigate()
  const { me, can } = useAuth()
  const toast = useToast()
  const [d, setD] = useState<Detail | null>(null)
  const [tab, setTab] = useState<TabKey>('assets')
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
      <Topbar crumbs={[<a onClick={() => nav('/products')} className="link">商品</a>, p.name]}>
        <button className="btn btn-ghost btn-sm" onClick={() => nav('/products')}><ArrowLeft />返回</button>
      </Topbar>
      <div className="scroll"><main className="content">
        <PageHead title={<span className="inline">{p.name}<Pill kind={ps?.pill || 'pill-neutral'}>{ps?.label}</Pill></span>}
          sub={<><span className="mono">#{p.id}</span><span className="sep" />{photos.length} 张素材<span className="sep" />{p.listings.length} 个店铺版本<span className="sep" />更新于 {fmtAgo(p.updated_at)}</>}>
          {can('product.edit') && (
            <select className="input" style={{ width: 120 }} value={p.status} onChange={e => setStatus(e.target.value)}>
              {Object.entries(productStatus).map(([k, v]) => <option key={k} value={k}>{v.label}</option>)}
            </select>
          )}
          {(can('product.delete_any') || (can('product.delete_own') && p.created_by === me?.user.id)) &&
            <button className="btn btn-secondary" onClick={remove}><Trash2 />删除</button>}
        </PageHead>
        <Tabs value={tab} onChange={setTab} items={[
          { key: 'assets', label: '素材', count: photos.length },
          { key: 'listings', label: '店铺版本', count: p.listings.length },
          { key: 'info', label: '基础信息' }
        ]} />
        {tab === 'assets' && <AssetsTab product={p} assets={photos} onChange={load} />}
        {tab === 'listings' && (
          <div className="stack">
            {p.listings.length === 0 && <section className="panel"><Empty title="还没有店铺版本" desc="加一个店铺版本，才能写文案和出图。" /></section>}
            {p.listings.map(l => <ListingCard key={l.id} listing={l} shopName={myShops.find(s => s.id === l.shop_id)?.name || `店铺 ${l.shop_id}`} onChange={load} />)}
            {can('product.edit') && missingShops.length > 0 && (
              <div className="toolbar">{missingShops.map(s => (
                <button key={s.id} className="btn btn-secondary" onClick={() => addListing(s.id)}><Plus />加到{s.name}<span className="t4">{platformName[s.platform]}</span></button>
              ))}</div>
            )}
          </div>
        )}
        {tab === 'info' && <InfoTab product={p} />}
      </main></div>
    </>
  )
}

function AssetsTab({ product, assets, onChange }: { product: Product; assets: Asset[]; onChange: () => void }) {
  const { can } = useAuth()
  const toast = useToast()
  const input = useRef<HTMLInputElement>(null)
  const [over, setOver] = useState(false)
  const [busy, setBusy] = useState(false)
  const upload = async (files: FileList | File[]) => {
    const list = Array.from(files).filter(f => /image\/(jpeg|png|webp)/.test(f.type))
    if (!list.length) return toast('只认 jpg、png、webp 图片', 'error')
    const form = new FormData()
    list.forEach(f => form.append('files', f))
    setBusy(true)
    try { await api(`/api/products/${product.id}/assets`, { form }); toast(`已上传 ${list.length} 张`, 'ok'); onChange() }
    catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  return (
    <section className="panel">
      <div className="panel-head"><div className="titles"><div className="panel-title">素材</div><div className="panel-sub">上传实拍图后，在店铺版本里点「一键出图」会自动分拣编号、修图、写文案、出图</div></div></div>
      <div className="panel-pad stack">
        {can('asset.upload') && (
          <div className={'drop' + (over ? ' over' : '')} onClick={() => input.current?.click()}
            onDragOver={e => { e.preventDefault(); setOver(true) }} onDragLeave={() => setOver(false)}
            onDrop={e => { e.preventDefault(); setOver(false); upload(e.dataTransfer.files) }}>
            <Upload /><b>{busy ? '上传中…' : '点击或拖入实拍图'}</b>jpg / png / webp，单张不超过 25 MB，可以一次选多张
            <input ref={input} type="file" accept="image/jpeg,image/png,image/webp" multiple hidden onChange={e => e.target.files && upload(e.target.files)} />
          </div>
        )}
        {assets.length === 0 ? <Empty title="还没有素材" /> : (
          <div className="thumbs">{assets.map(a => {
            const src = sourceName[a.source_override || a.source] || sourceName.original
            return (
              <a key={a.id} className="thumb" href={a.url} target="_blank" rel="noreferrer" title={a.ai_note || a.orig_name}>
                <img src={a.url + '?w=240'} alt="" loading="lazy" />
                {src.label !== '实拍' && <span className="src"><Pill kind={src.pill} dot={false}>{src.label}</Pill></span>}
                <div className="meta"><span className={'code' + (a.code ? '' : ' none')}>{a.code || '未编号'}</span><span className="grow" /><span className="t4 mono">{a.width}×{a.height}</span></div>
              </a>
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
              {[...out.main34, ...(out.white ? [out.white] : [])].map(u => (
                <a key={u} className="thumb" href={u} target="_blank" rel="noreferrer"><img src={u + '?w=240'} alt="" loading="lazy" /></a>
              ))}
            </div>
          </div>
        ) : !running && <div className="t3 fs-12">还没有成品。传好实拍图后点「一键出图」，一款大约 1–3 分钟。</div>}
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

function InfoTab({ product }: { product: Product }) {
  const info = product.info || {}
  const label: Record<string, string> = { series: '系列', category: '品类', pearl: '珍珠', shape: '形状', size: '尺寸', metal: '配件', colors: '颜色', price: '售价', highlights: '卖点', notes: '备注' }
  const rows = Object.entries(info).filter(([k, v]) => label[k] && v !== '' && v != null && !(Array.isArray(v) && v.length === 0))
  return (
    <div className="two-col">
      <section className="panel">
        <div className="panel-head"><div className="panel-title">基础信息</div></div>
        <div className="panel-pad">
          {rows.length === 0 ? <div className="t3">还没填，一键出图时 AI 会看照片补全。</div> : (
            <dl className="kv">{rows.map(([k, v]) => <Fragment key={k}><dt>{label[k]}</dt><dd className={k === 'price' ? 'mono' : ''}>{Array.isArray(v) ? v.join('、') : k === 'price' ? money(v) : String(v)}</dd></Fragment>)}</dl>
          )}
        </div>
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
    </div>
  )
}
