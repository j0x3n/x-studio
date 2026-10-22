import { useCallback, useEffect, useRef, useState, type DragEvent, type ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'
import { Upload, Sparkles, Wand2, History, Check, X, ImagePlus, Film, Play, Shirt, ScanSearch, AlertTriangle } from 'lucide-react'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { useJobs, jobRunning } from '../lib/jobs'
import { fmtAgo, fmtDuration, sourceName } from '../lib/format'
import { useCodeName } from '../lib/categories'
import { viewsApi, GROUP_NAME, isPending, blurry, type ProductViews as PV, type ViewAsset, type ViewSlot } from '../lib/views'
import type { Asset, Product } from '../lib/types'
import { Drawer, Empty, Loading, Pill } from './ui'
import { useLightbox } from './Lightbox'

// 商品页「底图」：按品类配置的槽位放实拍，整批拖进来让 AI 分槽位，糊的图多选高清化，候选在待确认区采用或丢弃。
// 版本抽屉里能把任意旧版本（包括最初的原图）换回当前。
const OK_TYPES = /image\/(jpeg|png|webp)/
const DRAG_TYPE = 'text/x-xs-asset'

export function ProductViews({ product, videos, onChange }: { product: Product; videos: Asset[]; onChange: () => void }) {
  const { me, can } = useAuth()
  const nav = useNavigate()
  const toast = useToast()
  const lightbox = useLightbox()
  const [d, setD] = useState<PV | null>(null)
  const [error, setError] = useState('')
  const [picked, setPicked] = useState<Set<number>>(new Set())
  const [mode, setMode] = useState<'confirm' | 'auto'>('confirm')
  const [busy, setBusy] = useState(false)
  const [jobIds, setJobIds] = useState<number[]>([])
  const [history, setHistory] = useState<ViewSlot | null>(null)
  const [overBatch, setOverBatch] = useState(false)
  const batchInput = useRef<HTMLInputElement>(null)
  const upload = can('asset.upload')
  const upscale = can('asset.upscale')
  const load = useCallback(() => viewsApi.get(product.id).then(r => { setD(r); setError('') }).catch(e => setError(errText(e))), [product.id])
  useEffect(() => { load() }, [load])

  // 分槽位、高清化在后台跑，跑完刷新
  const jobs = useJobs(jobIds)
  const running = jobIds.map(id => jobs[id]).filter(j => !j || jobRunning(j))
  const doneKey = jobIds.filter(id => jobs[id] && !jobRunning(jobs[id])).join(',')
  useEffect(() => {
    if (!doneKey) return
    load(); onChange()
    // 任务整体完成时，单张失败写在 result.items[].error 里
    doneKey.split(',').map(Number).forEach(id => {
      const j = jobs[id]
      if (j?.status === 'failed') return toast(j.message || 'AI 任务失败', 'error')
      const errs = ((j?.result?.items || []) as { error?: string }[]).map(i => i.error).filter(Boolean)
      if (errs.length) toast(`${errs.length} 张没做成：${[...new Set(errs)].join('；')}`, 'error')
      else toast(j?.kind === 'assets.views_enhance' ? '高清化做完了' : 'AI 分槽位做完了', 'ok')
    })
    setJobIds(ids => ids.filter(id => !doneKey.split(',').includes(String(id))))
  }, [doneKey]) // eslint-disable-line react-hooks/exhaustive-deps

  const act = async (fn: () => Promise<unknown>, ok?: string) => {
    setBusy(true)
    try { await fn(); if (ok) toast(ok, 'ok'); await load(); onChange() } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const images = (files: FileList | File[]) => {
    const list = Array.from(files).filter(f => OK_TYPES.test(f.type))
    if (!list.length) toast('底图只认 jpg、png、webp 图片；视频在下面「其他照片和视频」里传', 'error')
    return list
  }
  // 整批拖入：先上传（不指定槽位），再让 AI 分槽位
  const uploadBatch = async (files: FileList | File[]) => {
    const list = images(files)
    if (!list.length) return
    setBusy(true)
    try {
      await viewsApi.upload(product.id, list)
      const r = await viewsApi.classify(product.id)
      setJobIds(ids => [...ids, r.job_id])
      toast(`已上传 ${list.length} 张，AI 正在分槽位`, 'ok')
      await load(); onChange()
    } catch (e) { toast(errText(e), 'error'); load() } finally { setBusy(false) }
  }
  const uploadTo = (slot: ViewSlot, files: FileList | File[]) => {
    const list = images(files)
    if (list.length) act(() => viewsApi.upload(product.id, list, slot.code), `已放进「${slot.name}」`)
  }
  const classify = (ids?: number[]) => act(async () => { const r = await viewsApi.classify(product.id, ids); setJobIds(x => [...x, r.job_id]) }, 'AI 正在分槽位')
  const enhance = () => act(async () => {
    const r = await viewsApi.enhance(product.id, [...picked], mode)
    setJobIds(x => [...x, r.job_id]); setPicked(new Set())
  }, mode === 'auto' ? '正在高清化，核对通过的会自动替换' : '正在高清化，做好后在「待确认」里看')
  const assign = (a: ViewAsset, code: string) => act(() => viewsApi.setCode(a.id, code), '已放进槽位')
  const adopt = (a: ViewAsset) => act(() => viewsApi.makeCurrent(a.id), '已设为当前图')
  const discard = (a: ViewAsset, ask?: string) => { if (ask && !confirm(ask)) return; act(() => viewsApi.discard(a.id), '已丢弃') }
  const toggle = (id: number) => setPicked(old => { const n = new Set(old); if (n.has(id)) n.delete(id); else n.add(id); return n })

  // 从别处拖一张已有的照片到槽位 = 指定槽位；从电脑拖文件 = 上传到这个槽位
  const dropOn = (slot: ViewSlot) => (e: DragEvent) => {
    e.preventDefault()
    const id = Number(e.dataTransfer.getData(DRAG_TYPE))
    if (id) { if (id !== slot.current?.id) act(() => viewsApi.setCode(id, slot.code), `已放进「${slot.name}」`) }
    else if (e.dataTransfer.files.length) uploadTo(slot, e.dataTransfer.files)
  }
  const dragFrom = (a: ViewAsset) => upload ? { draggable: true, onDragStart: (e: DragEvent) => { e.dataTransfer.setData(DRAG_TYPE, String(a.id)); e.dataTransfer.effectAllowed = 'move' } } : {}
  const view = (list: ViewAsset[], i: number, title: (a: ViewAsset) => string) => lightbox(list.map(a => ({ src: a.url, title: title(a), meta: `${a.width}×${a.height}`, download: '' })), i)

  if (error) return <section className="panel"><Empty title="底图加载失败" desc={error}><button className="btn btn-secondary" onClick={load}>重试</button></Empty></section>
  if (!d) return <section className="panel"><Loading /></section>
  const slotName = (code: string) => d.views.find(v => v.code === code)?.name || code
  const pendings = [...d.views.flatMap(v => v.pending.map(a => ({ a, slot: v as ViewSlot | null }))), ...d.unassigned.filter(isPending).map(a => ({ a, slot: null }))]
  const loose = d.unassigned.filter(a => !isPending(a))
  const original = (a: ViewAsset) => [...d.views.flatMap(v => v.versions), ...d.unassigned, ...d.others].find(x => x.id === a.parent_id)
  const unclassified = loose.filter(a => !a.suggest?.code && !a.suggest?.reason).length
  const pickable = upscale && upload
  // 图片处理、试穿试戴从这里进（侧栏不再单列）
  const tools = (a: ViewAsset) => me?.role.scope !== 'shares' && can('asset.process') && can('asset.upload') ? <>
    <button type="button" className="btn btn-ghost btn-icon btn-sm" title="拿去图片处理台（修图、抠图、换比例）" aria-label="图片处理" onClick={() => nav('/process?assets=' + a.id)}><Wand2 /></button>
    {can('model.view') && <button type="button" className="btn btn-ghost btn-icon btn-sm" title="用这张试穿试戴" aria-label="试穿试戴" onClick={() => nav('/tryon?assets=' + a.id)}><Shirt /></button>}
  </> : null

  return <>
    <section className="panel">
      <div className="panel-head">
        <div className="titles">
          <div className="panel-title inline">底图<Pill kind={d.ready ? 'pill-success' : 'pill-warning'}>{d.ready ? '必传齐了' : `缺 ${d.missing.length} 个必传`}</Pill></div>
          <div className="panel-sub">{GROUP_NAME[d.group]} · 每个槽位放一张当前图；微距、尺寸、包装、模特位只用实拍和通过保真核对的高清图</div>
        </div>
        {upload && loose.length > 0 && <button className="btn btn-secondary btn-sm" disabled={busy} onClick={() => classify()}><ScanSearch />AI 分槽位{unclassified > 0 && <span className="t3">（{unclassified} 张没分）</span>}</button>}
      </div>
      <div className="panel-pad stack">
        {upload && <div className={'drop' + (overBatch ? ' over' : '')} onClick={() => batchInput.current?.click()}
          onDragOver={e => { if (!e.dataTransfer.types.includes(DRAG_TYPE)) { e.preventDefault(); setOverBatch(true) } }} onDragLeave={() => setOverBatch(false)}
          onDrop={e => { e.preventDefault(); setOverBatch(false); uploadBatch(e.dataTransfer.files) }}>
          <Upload /><b>{busy ? '处理中…' : '整批拖进货源实拍，AI 自动分到槽位'}</b>拿不准的放进「待确认」等你定；也可以直接把图拖到下面某个槽位上
          <input ref={batchInput} type="file" accept="image/jpeg,image/png,image/webp" multiple hidden onChange={e => { if (e.target.files) uploadBatch(e.target.files); e.target.value = '' }} />
        </div>}
        {running.length > 0 && <div className="views-job">
          {running.map((j, i) => <div key={j?.id || i} className="row between fs-12"><span className="inline"><Sparkles size={14} />{j?.kind === 'assets.views_enhance' ? '高清化和保真核对' : 'AI 分槽位'} · {j?.message || '排队中'}</span><span className="mono">{j?.progress || 0}%</span></div>)}
        </div>}
        <div className="views-grid">
          {d.views.map(v => <SlotCard key={v.key} slot={v} editable={upload} picked={!!v.current && picked.has(v.current.id)} onPick={pickable && v.current ? () => toggle(v.current!.id) : undefined}
            onDrop={dropOn(v)} onFiles={f => uploadTo(v, f)} onHistory={() => setHistory(v)} drag={v.current ? dragFrom(v.current) : {}} tools={v.current ? tools(v.current) : null}
            onView={() => v.current && view([v.current], 0, () => `${v.name} · 当前图`)} />)}
        </div>
        {pickable && <div className="views-bar">
          <span className="t3 fs-12">{picked.size ? `选了 ${picked.size} 张` : '勾选糊的图（槽位或下面没分的都行）做高清化'}</span>
          <span className="grow" />
          <div className="segmented"><button className={mode === 'confirm' ? 'active' : ''} onClick={() => setMode('confirm')}>做好后我来确认</button><button className={mode === 'auto' ? 'active' : ''} onClick={() => setMode('auto')}>核对通过自动替换</button></div>
          <button className="btn btn-primary btn-sm" disabled={busy || !picked.size} onClick={enhance}><Wand2 />高清化 {picked.size || ''}</button>
          {picked.size > 0 && <button className="btn btn-ghost btn-sm" onClick={() => setPicked(new Set())}>清空</button>}
        </div>}
      </div>
    </section>

    {pendings.length > 0 && <section className="panel">
      <div className="panel-head"><div className="titles"><div className="panel-title inline">待确认<span className="badge">{pendings.length}</span></div><div className="panel-sub">高清化候选：左边原图、右边高清图。保真分不够或有问题项的不会自动替换，采用后也不能进证据位</div></div></div>
      <div className="panel-pad views-pending">
        {pendings.map(({ a, slot }) => {
          const before = original(a)
          const c = a.enhance_check || {}
          const pass = c.verdict === 'pass'
          return <article key={a.id} className="views-cand">
            <div className="views-cand-pair">
              <button type="button" className="zoomable" onClick={() => lightbox([...(before ? [{ src: before.url, title: '原图', meta: `${before.width}×${before.height}`, download: '' }] : []), { src: a.url, title: '高清候选', meta: `${a.width}×${a.height}`, download: '' }], before ? 1 : 0)}>
                {before ? <img src={before.url + '?w=240'} alt="原图" /> : <span className="views-blank">原图已不在</span>}
                <img src={a.url + '?w=240'} alt="高清候选" />
              </button>
              <span className="views-cand-tag l">原图</span><span className="views-cand-tag r">高清</span>
            </div>
            <div className="stack" style={{ gap: 4 }}>
              <div className="row" style={{ gap: 6 }}>
                <b className="fs-13">{slot ? slot.name : '还没分槽位'}</b>
                <Pill kind={pass ? 'pill-success' : 'pill-warning'}>{pass ? '核对通过' : '要人工看'}</Pill>
                {typeof c.fidelity === 'number' && <span className="mono fs-12 t2">保真 {c.fidelity}</span>}
              </div>
              {typeof c.blur_before === 'number' && <div className="fs-12 t3">清晰度 <span className="mono">{Math.round(c.blur_before)}</span> → <span className="mono t1">{Math.round(c.blur_after || 0)}</span>（越大越清楚）</div>}
              {(c.issues || []).map((s, i) => <div key={i} className="fs-12 views-issue"><AlertTriangle size={12} />{s}</div>)}
              {upload && <div className="row" style={{ gap: 6, marginTop: 2 }}>
                {slot ? <button className="btn btn-primary btn-sm" disabled={busy} onClick={() => adopt(a)}><Check />采用</button>
                  : <SlotSelect views={d.views} label="采用并放进…" disabled={busy} onPick={code => assign(a, code)} />}
                <button className="btn btn-ghost btn-sm" disabled={busy} onClick={() => discard(a)}><X />丢弃</button>
              </div>}
            </div>
          </article>
        })}
      </div>
    </section>}

    {loose.length > 0 && <section className="panel">
      <div className="panel-head"><div className="titles"><div className="panel-title inline">还没分槽位<span className="badge neutral">{loose.length}</span></div><div className="panel-sub">AI 拿不准的会写上建议；拖到上面的槽位或者在下拉里选</div></div></div>
      <div className="panel-pad">
        <div className="thumbs views-loose-grid">{loose.map((a, i) => {
          const s = a.suggest || {}
          return <div key={a.id} className={'thumb views-loose' + (picked.has(a.id) ? ' picked' : '')} {...dragFrom(a)}>
            <button type="button" className="zoomable" onClick={() => view(loose, i, x => x.orig_name)}><img src={a.url + '?w=240'} alt={a.orig_name} loading="lazy" /></button>
            {pickable && <input type="checkbox" className="check views-check" checked={picked.has(a.id)} onChange={() => toggle(a.id)} aria-label="选中" />}
            {blurry(a) && <span className="src"><Pill kind="pill-warning" dot={false}>模糊</Pill></span>}
            <div className="views-suggest fs-12">
              {s.code ? <>AI 建议：<b>{slotName(s.code)}</b>{typeof s.confidence === 'number' && <span className="mono t3"> {Math.round(s.confidence * 100)}%</span>}</>
                : s.reason ? <span className="t3">AI 没认出槽位</span> : <span className="t4">还没让 AI 看</span>}
              {s.reason && <div className="t3 views-reason" title={s.reason}>{s.reason}</div>}
            </div>
            {upload && <div className="meta">
              {s.code && <button className="btn btn-secondary btn-sm" disabled={busy} onClick={() => assign(a, s.code!)}>采用建议</button>}
              <SlotSelect views={d.views} label={s.code ? '放别处' : '放进槽位…'} disabled={busy} onPick={code => assign(a, code)} />
              <span className="grow" />
              <button className="btn btn-ghost btn-icon btn-sm" title="丢掉这张（文件保留，不再显示）" aria-label="丢弃" disabled={busy} onClick={() => discard(a, '丢掉这张照片？文件保留但不再显示，也不会用来出图。')}><X /></button>
            </div>}
          </div>
        })}</div>
      </div>
    </section>}

    <OtherAssets product={product} photos={d.others} videos={videos} onChange={() => { load(); onChange() }} />
    {history && <VersionDrawer slot={d.views.find(v => v.key === history.key) || history} productName={product.name} editable={upload} busy={busy} onClose={() => setHistory(null)} onAdopt={adopt} onDiscard={a => discard(a, a.id === (d.views.find(v => v.key === history.key)?.current?.id) ? '丢掉当前图？这个槽位会空出来，可以再从版本里换一张回来。' : '丢掉这个版本？文件保留但不再显示。')} />}
  </>
}

function SlotSelect({ views, label, disabled, onPick }: { views: ViewSlot[]; label: string; disabled?: boolean; onPick: (code: string) => void }) {
  return <select className="input input-sm views-select" value="" disabled={disabled} onChange={e => e.target.value && onPick(e.target.value)} aria-label={label}>
    <option value="">{label}</option>
    {views.map(v => <option key={v.key} value={v.code}>{v.name}{v.current ? '（替换当前）' : ''}</option>)}
  </select>
}

function SlotCard({ slot, editable, picked, onPick, onDrop, onFiles, onHistory, onView, drag, tools }: {
  slot: ViewSlot; editable: boolean; picked: boolean; onPick?: () => void; onDrop: (e: DragEvent) => void; onFiles: (f: FileList) => void
  onHistory: () => void; onView: () => void; drag: object; tools: ReactNode
}) {
  const input = useRef<HTMLInputElement>(null)
  const [over, setOver] = useState(false)
  const c = slot.current
  const src = c ? sourceName[c.source_override || c.source] || sourceName.original : null
  const verified = c?.enhance_check?.verdict === 'pass'
  return <div className={'views-slot' + (over ? ' over' : '') + (!c && slot.required ? ' missing' : '') + (picked ? ' picked' : '')}
    onDragOver={editable ? e => { e.preventDefault(); setOver(true) } : undefined} onDragLeave={() => setOver(false)} onDrop={e => { setOver(false); if (editable) onDrop(e) }}>
    <div className="views-slot-head">
      <b className="ellipsis">{slot.name}</b>
      {slot.required && <span className={'views-req' + (c ? '' : ' on')}>必传</span>}
      <span className="grow" /><span className="mono t4">{slot.code}</span>
    </div>
    {c ? <div className="views-slot-img" {...drag}>
      <button type="button" className="zoomable" onClick={onView} title={slot.hint}><img src={c.url + '?w=480'} alt={slot.name} loading="lazy" /></button>
      {onPick && <input type="checkbox" className="check views-check" checked={picked} onChange={onPick} aria-label="选中做高清化" />}
      <span className="views-flags">
        {blurry(c) && <Pill kind="pill-warning" dot={false}>模糊</Pill>}
        {src && src.label !== '原图' && <Pill kind={verified ? 'pill-success' : c.enhance_check?.verdict ? 'pill-warning' : src.pill} dot={false}>{verified ? '高清 · 已核对' : c.enhance_check?.verdict ? '高清 · 未过核对' : src.label}</Pill>}
      </span>
    </div> : <button type="button" className="views-slot-img views-empty" disabled={!editable} onClick={() => input.current?.click()}>
      <ImagePlus /><span>{editable ? '点选或拖入' : '还没有'}</span><span className="views-hint">{slot.hint}</span>
    </button>}
    <div className="views-slot-foot fs-12">
      {c ? <span className="t4 mono">{c.width}×{c.height}</span> : <span className="t4">空</span>}
      {slot.pending.length > 0 && <Pill kind="pill-info" dot={false}>{slot.pending.length} 张待确认</Pill>}
      <span className="grow" />
      {tools}
      {editable && c && <button type="button" className="btn btn-ghost btn-icon btn-sm" title="换一张" aria-label="换一张" onClick={() => input.current?.click()}><Upload /></button>}
      {slot.versions.length > 0 && <button type="button" className="btn btn-ghost btn-sm" onClick={onHistory} title="看这个槽位的所有版本，能换回原图"><History />{slot.versions.length}</button>}
    </div>
    <input ref={input} type="file" accept="image/jpeg,image/png,image/webp" hidden onChange={e => { if (e.target.files?.length) onFiles(e.target.files); e.target.value = '' }} />
  </div>
}

function VersionDrawer({ slot, productName, editable, busy, onClose, onAdopt, onDiscard }: { slot: ViewSlot; productName: string; editable: boolean; busy: boolean; onClose: () => void; onAdopt: (a: ViewAsset) => void; onDiscard: (a: ViewAsset) => void }) {
  const lightbox = useLightbox()
  const list = slot.versions
  const first = list.length ? list.reduce((a, b) => (a.created_at < b.created_at || (a.created_at === b.created_at && a.id < b.id) ? a : b)) : null
  return <Drawer title={<span className="inline"><History size={16} />{slot.name} · 版本</span>} width={520} onClose={onClose}>
    <div className="help">新的在前。换回任意一张（包括最初的原图）都行；丢弃的只是不再显示，文件还在。</div>
    {!list.length ? <Empty title="还没有版本" /> : <div className="stack" style={{ gap: 8 }}>
      {list.map((a, i) => {
        const cur = a.id === slot.current?.id
        const src = sourceName[a.source_override || a.source] || sourceName.original
        const c = a.enhance_check || {}
        return <div key={a.id} className={'views-ver' + (cur ? ' on' : '')}>
          <button type="button" className="zoomable" onClick={() => lightbox(list.map(x => ({ src: x.url, title: `${productName} · ${slot.name}`, meta: `${x.width}×${x.height}`, download: '' })), i)}><img src={a.url + '?w=240'} alt="" /></button>
          <div className="grow stack" style={{ gap: 3, minWidth: 0 }}>
            <div className="row" style={{ gap: 6 }}>
              {cur && <Pill kind="pill-info" dot={false}>当前</Pill>}
              {a.id === first?.id && <Pill kind="pill-neutral" dot={false}>最初的原图</Pill>}
              <Pill kind={src.pill} dot={false}>{src.label}</Pill>
              {isPending(a) && <Pill kind="pill-warning" dot={false}>待确认</Pill>}
              {blurry(a) && <Pill kind="pill-warning" dot={false}>模糊</Pill>}
            </div>
            <div className="fs-12 t3"><span className="mono">{a.width}×{a.height}</span> · {fmtAgo(a.created_at)}{typeof c.fidelity === 'number' && <> · 保真 <span className="mono">{c.fidelity}</span>{c.verdict === 'pass' ? '（通过）' : '（未通过）'}</>}</div>
          </div>
          {editable && !cur && <button className="btn btn-secondary btn-sm" disabled={busy} onClick={() => onAdopt(a)}>设为当前</button>}
          {editable && <button className="btn btn-ghost btn-icon btn-sm" title="丢弃" aria-label="丢弃" disabled={busy} onClick={() => onDiscard(a)}><X /></button>}
        </div>
      })}
    </div>}
  </Drawer>
}

// 不属于当前分组槽位的旧照片（微距、尺寸、包装、上身……）和视频，保留原来的传图方式
function OtherAssets({ product, photos, videos, onChange }: { product: Product; photos: ViewAsset[]; videos: Asset[]; onChange: () => void }) {
  const lightbox = useLightbox()
  const nav = useNavigate()
  const { me, can } = useAuth()
  const toast = useToast()
  const codeName = useCodeName()
  const input = useRef<HTMLInputElement>(null)
  const [over, setOver] = useState(false)
  const [busy, setBusy] = useState(false)
  const all: Asset[] = [...photos, ...videos]
  const upload = async (files: FileList | File[]) => {
    const list = Array.from(files).filter(f => /image\/(jpeg|png|webp)|video\/(mp4|quicktime|webm)/.test(f.type))
    if (!list.length) return toast('只认 jpg、png、webp 图片和 mp4、mov、webm 视频', 'error')
    if (list.some(f => f.type.startsWith('video/') && f.size > 500 * 1024 * 1024)) return toast('视频不能超过 500 MB', 'error')
    setBusy(true)
    try { await viewsApi.upload(product.id, list); toast(`已上传 ${list.length} 个`, 'ok'); onChange() }
    catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const box = (a: Asset) => a.media === 'video'
    ? { src: a.url, video: true, poster: a.poster_url, title: a.title || a.orig_name, meta: fmtDuration(a.duration_ms), download: '' }
    : { src: a.url, title: a.title || a.orig_name, meta: `${a.width}×${a.height}`, download: '' }
  const tools = me?.role.scope !== 'shares'
  return <section className="panel">
    <div className="panel-head">
      <div className="titles"><div className="panel-title">其他照片和视频</div><div className="panel-sub">微距、尺寸、包装、上身等证据照和视频；照片传上来没进槽位的也会先出现在「还没分槽位」</div></div>
      {can('asset.upload') && <button className="btn btn-secondary btn-sm" disabled={busy} onClick={() => input.current?.click()}><Upload />{busy ? '上传中…' : '传视频或其他照片'}</button>}
      <input ref={input} type="file" accept="image/jpeg,image/png,image/webp,video/mp4,video/quicktime,video/webm" multiple hidden onChange={e => { if (e.target.files) upload(e.target.files); e.target.value = '' }} />
    </div>
    <div className="panel-pad" onDragOver={e => { if (can('asset.upload') && !e.dataTransfer.types.includes(DRAG_TYPE)) { e.preventDefault(); setOver(true) } }} onDragLeave={() => setOver(false)} onDrop={e => { e.preventDefault(); setOver(false); upload(e.dataTransfer.files) }}>
      {all.length === 0 ? <div className={'t3 fs-12 views-other-empty' + (over ? ' over' : '')}>没有。视频（mp4 / mov / webm，不超过 500 MB）可以拖到这里。</div> : (
        <div className={'thumbs' + (over ? ' views-over' : '')}>{all.map((a, ai) => {
          const src = sourceName[a.source_override || a.source] || sourceName.original
          return <div key={a.id} className="thumb product-tryon-thumb">
            <button type="button" className="zoomable" title={a.ai_note || a.orig_name} onClick={() => lightbox(all.map(box), ai)}>
              {a.media === 'video'
                ? (a.poster_url ? <img src={a.poster_url} alt={a.orig_name} loading="lazy" /> : <span className="lib-video-blank"><Film /></span>)
                : <img src={a.url + '?w=240'} alt={a.orig_name} loading="lazy" />}
              {a.media === 'video' && <span className="lib-video-badge mono"><Play />{fmtDuration(a.duration_ms) || '视频'}</span>}
            </button>
            {src.label !== '原图' && <span className="src"><Pill kind={src.pill} dot={false}>{src.label}</Pill></span>}
            <div className="meta">
              {a.media === 'video' ? <span className="code">视频</span> : <span className={'code' + (a.code ? '' : ' none')} title={a.code ? `拍摄编号 ${a.code}` : ''}>{a.code ? codeName(a.code, product.category_id) : '未分类'}</span>}
              <span className="grow" />{a.width > 0 && <span className="t4 mono">{a.width}×{a.height}</span>}
            </div>
            {a.media !== 'video' && tools && <div className="row" style={{ gap: 0 }}>
              {can('asset.process') && can('asset.upload') && <button className="btn btn-ghost btn-sm" onClick={() => nav('/process?assets=' + a.id)}><Wand2 />处理</button>}
              {can('model.view') && can('asset.process') && can('asset.upload') && <button className="btn btn-ghost btn-sm" onClick={() => nav('/tryon?assets=' + a.id)}><Shirt />试戴</button>}
            </div>}
          </div>
        })}</div>
      )}
    </div>
  </section>
}
