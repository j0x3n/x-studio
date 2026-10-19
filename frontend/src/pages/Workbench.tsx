import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import {
  Maximize2, Sparkles, Scissors, Palette, Crop, Ruler, Upload, Images, X, Download, Save, Check, Loader2,
  Wand2, Columns2, SplitSquareHorizontal, RotateCw, Search, ImageOff, ImagePlus
} from 'lucide-react'
import { api, downloadPost, qs, type Page } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { useJobs, jobRunning } from '../lib/jobs'
import type { Asset, Product, ProcessOps, WbItem } from '../lib/types'
import { Topbar, Pill, Loading, Empty, Drawer, Field } from '../components/ui'
import { sourceName, assetTitle, quickOps, imgUrl, opName, fmtAgo } from '../lib/format'

// 一张待处理的图：来自素材库，或者上传 / 粘贴进来的
type Src = { key: string; asset?: Asset; item?: WbItem }
type ToolKey = 'upscale' | 'ai' | 'cutout' | 'color' | 'crop' | 'resize'
type Opts = { upscale: number; quick: string[]; extra: string; cutout: 'white' | 'transparent'; autoWB: boolean; matchTo: number; rotate: number; aspect: string; resize: number; format: 'jpg' | 'png' }

const TOOLS: { key: ToolKey; label: string; desc: string; icon: typeof Wand2; perm: string; ai?: boolean }[] = [
  { key: 'upscale', label: '高清化', desc: 'AI 重绘后放大', icon: Maximize2, perm: 'asset.upscale', ai: true },
  { key: 'ai', label: 'AI 改图', desc: '去灰尘、换背景、按要求改', icon: Sparkles, perm: 'asset.process', ai: true },
  { key: 'cutout', label: '抠图', desc: '白底或透明背景', icon: Scissors, perm: 'asset.upload' },
  { key: 'color', label: '调色', desc: '白平衡、统一色调', icon: Palette, perm: 'asset.upload' },
  { key: 'crop', label: '裁剪旋转', desc: '旋转、按比例裁', icon: Crop, perm: 'asset.upload' },
  { key: 'resize', label: '尺寸格式', desc: '改长边、转格式', icon: Ruler, perm: 'asset.upload' }
]
const DEFAULT: Opts = { upscale: 2048, quick: [], extra: '', cutout: 'white', autoWB: true, matchTo: 0, rotate: 0, aspect: 'free', resize: 1600, format: 'jpg' }
const RATIOS: Record<string, number> = { '1:1': 1, '3:4': 3 / 4, '4:3': 4 / 3, '16:9': 16 / 9 }
const RANK: Record<string, number> = { original: 0, ai_enhanced: 1, ai_edited: 2, ai_generated: 3 }

function opsFor(tool: ToolKey, o: Opts): ProcessOps | null {
  switch (tool) {
    case 'upscale': return { upscale: { target_long_side: o.upscale }, format: 'jpg' }
    case 'ai': return o.quick.length || o.extra.trim() ? { ai: { quick: o.quick, extra: o.extra.trim() }, format: 'jpg' } : null
    case 'cutout': return { cutout: { background: o.cutout }, format: o.cutout === 'transparent' ? 'png' : 'jpg' }
    case 'color': return o.autoWB || o.matchTo ? { color: { ...(o.autoWB ? { auto_wb: true } : {}), ...(o.matchTo ? { match_to: o.matchTo } : {}) }, format: 'jpg' } : null
    case 'crop': return o.rotate || o.aspect !== 'free' ? { ...(o.rotate ? { geometry: { rotate: o.rotate } } : {}), ...(o.aspect !== 'free' ? { aspect: o.aspect as ProcessOps['aspect'] } : {}), format: 'jpg' } : null
    case 'resize': return { ...(o.resize ? { resize: { long_side: o.resize } } : {}), format: o.format }
  }
}
// 这次处理本身会把结果算成什么来源（再和原图的来源取更 AI 的那个）
function toolSource(tool: ToolKey, o: Opts) {
  if (tool === 'ai') return o.extra.trim() || o.quick.some(k => quickOps.find(q => q.key === k)?.heavy) ? 'ai_edited' : 'ai_enhanced'
  if (tool === 'upscale') return 'ai_enhanced'
  return 'original'
}
const srcSource = (s: Src) => s.asset ? (s.asset.effective_source || s.asset.source_override || s.asset.source) : (s.item?.source || 'original')
const srcName = (s: Src) => s.asset ? assetTitle(s.asset) : (s.item?.orig_name || '粘贴的图')
const srcUrl = (s: Src, w: 240 | 480 | 960) => s.asset ? imgUrl(s.asset, w) : wbUrl(s.item!, w)
const srcSize = (s: Src) => s.asset ? [s.asset.width, s.asset.height] : [s.item?.width || 0, s.item?.height || 0]
const wbUrl = (i: WbItem, w: 240 | 480 | 960) => /\.png$/i.test(i.orig_name) && i.kind === 'input' ? i.url : `${i.url}?w=${w}`
const resultTool = (r: WbItem) => Object.keys(r.op?.ops || {}).filter(k => k !== 'format').map(k => opName[k] || k).join(' + ') || '转格式'
// 结果是不是由这张图处理出来的
const fromSrc = (r: WbItem, s: Src) => (s.asset && r.asset_id === s.asset.id) || (s.item && r.input_id === s.item.id)

export function Workbench() {
  const { can } = useAuth()
  const toast = useToast()
  const nav = useNavigate()
  const [sp, setSp] = useSearchParams()
  const [srcs, setSrcs] = useState<Src[]>([])
  const [results, setResults] = useState<WbItem[] | null>(null)
  const [focus, setFocus] = useState<{ src?: string; result?: number }>({})
  const [tool, setTool] = useState<ToolKey>(can('asset.upscale') ? 'upscale' : 'cutout')
  const [o, setO] = useState<Opts>(DEFAULT)
  const [scope, setScope] = useState<'one' | 'all'>('all')
  const [jobIds, setJobIds] = useState<number[]>([])
  const [lastRun, setLastRun] = useState<number[]>([])
  const [busy, setBusy] = useState(false)
  const [uploading, setUploading] = useState(false)
  const [picking, setPicking] = useState(false)
  const [saving, setSaving] = useState<WbItem | null>(null)
  const [view, setView] = useState<'slide' | 'side'>('slide')
  const [over, setOver] = useState(false)
  const fileRef = useRef<HTMLInputElement>(null)
  const jobs = useJobs(jobIds)

  const loadResults = useCallback(() => api<Page<WbItem>>('/api/workbench/items' + qs({ kind: 'result', operation: 'process' })).then(r => setResults(r.items)).catch(() => setResults([])), [])
  useEffect(() => { loadResults() }, [loadResults])

  const addAssets = useCallback((list: Asset[]) => {
    setSrcs(prev => {
      const have = new Set(prev.map(s => s.key))
      const add = list.filter(a => !have.has('a' + a.id)).map(a => ({ key: 'a' + a.id, asset: a }))
      return [...prev, ...add]
    })
    if (list[0]) setFocus({ src: 'a' + list[0].id })
  }, [])
  // 从素材库带过来的图：/process?assets=1,2
  useEffect(() => {
    const ids = (sp.get('assets') || sp.get('ids') || '').split(',').map(Number).filter(n => n > 0)
    if (!ids.length) return
    Promise.all(ids.map(id => api<{ asset: Asset }>(`/api/assets/${id}`).then(r => r.asset).catch(() => null)))
      .then(list => addAssets(list.filter((a): a is Asset => !!a)))
    setSp(new URLSearchParams(), { replace: true })
  }, [sp, setSp, addAssets])

  const upload = useCallback(async (files: File[]) => {
    const list = files.filter(f => /image\/(jpeg|png|webp)/.test(f.type))
    if (!list.length) { toast('只认 jpg、png、webp 图片', 'error'); return }
    if (list.length > 20) { toast('一次最多 20 张', 'error'); return }
    const form = new FormData()
    list.forEach((f, i) => form.append('files', f, f.name || `粘贴的图-${i + 1}.png`))
    setUploading(true)
    try {
      const r = await api<{ items: WbItem[] }>('/api/workbench/inputs', { form })
      setSrcs(prev => [...prev, ...r.items.map(item => ({ key: 'u' + item.id, item }))])
      if (r.items[0]) setFocus({ src: 'u' + r.items[0].id })
    } catch (e) { toast(errText(e), 'error') } finally { setUploading(false) }
  }, [toast])
  // 在页面任何地方 Ctrl+V 粘贴图片
  useEffect(() => {
    const f = (e: ClipboardEvent) => {
      const t = e.target as HTMLElement | null
      if (t && (t.tagName === 'TEXTAREA' || (t.tagName === 'INPUT' && (t as HTMLInputElement).type === 'text'))) return
      const files = Array.from(e.clipboardData?.files || []).filter(x => x.type.startsWith('image/'))
      if (files.length) { e.preventDefault(); upload(files) }
    }
    addEventListener('paste', f)
    return () => removeEventListener('paste', f)
  }, [upload])

  // 任务结束：刷新结果，定位到这次的第一张
  const running = jobIds.some(id => !jobs[id] || jobRunning(jobs[id]))
  const doneKey = jobIds.map(id => jobs[id]?.status).join(',')
  useEffect(() => {
    if (!jobIds.length || running) return
    const items = jobIds.flatMap(id => ((jobs[id]?.result?.items as { result_id?: number; error?: string }[] | undefined) || []))
    const ok = items.map(i => i.result_id).filter((x): x is number => !!x)
    const bad = items.filter(i => i.error)
    const failed = jobIds.map(id => jobs[id]).find(j => j?.status === 'failed')
    if (failed && !items.length) toast(failed.message || '处理失败', 'error')
    else toast(bad.length ? `处理完 ${ok.length} 张，${bad.length} 张失败：${bad[0].error}` : `处理完 ${ok.length} 张`, bad.length ? 'error' : 'ok')
    setLastRun(ok)
    loadResults().then(() => { if (ok[0]) setFocus({ result: ok[0] }) })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [doneKey, running])

  const cur = srcs.find(s => s.key === focus.src)
  const curResult = results?.find(r => r.id === focus.result)
  const resultSrc = curResult ? srcs.find(s => fromSrc(curResult, s)) : undefined
  const targets = scope === 'all' ? srcs : cur ? [cur] : []
  const t = TOOLS.find(x => x.key === tool)!
  const ops = opsFor(tool, o)
  const predicted = targets.length ? targets.map(s => [srcSource(s), toolSource(tool, o)].sort((a, b) => RANK[b] - RANK[a])[0]).sort((a, b) => RANK[b] - RANK[a])[0] : toolSource(tool, o)
  const assetSrcs = srcs.filter(s => s.asset)
  const upd = (p: Partial<Opts>) => setO(v => ({ ...v, ...p }))

  const run = async () => {
    if (!ops || !targets.length) return
    setBusy(true)
    try {
      const inputs = targets.map(s => s.asset ? { asset_id: s.asset.id } : { input_id: s.item!.id })
      const r = await api<{ job: { id: number } }>('/api/workbench/process', { body: { inputs, ops } })
      setJobIds([r.job.id]); setLastRun([])
    } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const removeSrc = (key: string) => { setSrcs(v => v.filter(s => s.key !== key)); if (focus.src === key) setFocus({}) }
  const removeResult = async (r: WbItem) => {
    try { await api(`/api/workbench/items/${r.id}`, { method: 'DELETE' }); if (focus.result === r.id) setFocus({}); loadResults() } catch (e) { toast(errText(e), 'error') }
  }
  const lastResults = (results || []).filter(r => lastRun.includes(r.id))
  const saveAllVersions = async () => {
    const list = lastResults.filter(r => r.asset && !r.saved_asset_id)
    let ok = 0
    for (const r of list) { try { await api(`/api/workbench/items/${r.id}/save`, { body: {} }); ok++ } catch (e) { toast(errText(e), 'error'); break } }
    if (ok) toast(`已把 ${ok} 张存成素材库里原图的新版本`, 'ok')
    loadResults()
  }
  const downloadAll = async (ids: number[]) => {
    try { await downloadPost('/api/workbench/download', { ids }, `处理结果_${ids.length}张.zip`) } catch (e) { toast(errText(e), 'error') }
  }

  return (
    <>
      <Topbar crumbs={['图片处理']} />
      <div className="lib">
        <aside className="sidebar proc-list" onDragOver={e => { e.preventDefault(); setOver(true) }} onDragLeave={() => setOver(false)}
          onDrop={e => { e.preventDefault(); setOver(false); upload(Array.from(e.dataTransfer.files)) }}>
          <div className="nav-label">待处理<span className="end">{srcs.length}</span></div>
          <div className="wb-add">
            <button className="btn btn-secondary btn-sm" disabled={uploading} onClick={() => fileRef.current?.click()}><Upload />{uploading ? '上传中…' : '上传'}</button>
            <button className="btn btn-secondary btn-sm" onClick={() => setPicking(true)}><Images />从素材库选</button>
            <input ref={fileRef} type="file" accept="image/jpeg,image/png,image/webp" multiple hidden onChange={e => { e.target.files && upload(Array.from(e.target.files)); e.target.value = '' }} />
          </div>
          {srcs.length === 0 ? (
            <div className={'drop' + (over ? ' over' : '')} onClick={() => fileRef.current?.click()}>
              <ImagePlus /><b>拖进来、Ctrl+V 粘贴</b>或点上面的按钮添加图片
            </div>
          ) : srcs.map(s => {
            const busyHere = running && jobIds.length > 0 && targets.some(x => x.key === s.key)
            const has = results?.some(r => fromSrc(r, s))
            return (
              <div key={s.key} className={'proc-item' + (focus.src === s.key ? ' active' : '')} onClick={() => setFocus({ src: s.key })}>
                <img src={srcUrl(s, 240)} alt="" />
                <div style={{ minWidth: 0, flex: 1 }}>
                  <div className="ellipsis fs-12" style={{ fontWeight: 500 }}>{s.asset?.code && <span className="mono">{s.asset.code} · </span>}{srcName(s)}</div>
                  <div className="ellipsis fs-12 t3">{s.asset ? (s.asset.product?.name || `款 #${s.asset.product_id}`) : '上传的图'}</div>
                </div>
                {busyHere ? <Loader2 className="st spin" /> : has ? <Check className="st ok" /> : null}
                <button className="tag-x" title="移出" onClick={e => { e.stopPropagation(); removeSrc(s.key) }}><X /></button>
              </div>
            )
          })}
          {srcs.length > 0 && <div className="t4 fs-12" style={{ padding: '0 6px' }}>也可以拖进来或 Ctrl+V 粘贴</div>}

          <div className="nav-label" style={{ marginTop: 8 }}>处理结果<span className="end">{results?.length ?? ''}</span></div>
          {!results ? <Loading /> : results.length === 0 ? <div className="t4 fs-12" style={{ padding: '0 6px' }}>处理完的图会放在这里，保存或下载前不会进素材库，7 天后自动清掉。</div>
            : results.slice(0, 60).map(r => {
              const rs = sourceName[r.source] || sourceName.original
              return (
                <div key={r.id} className={'proc-item' + (focus.result === r.id ? ' active' : '')} onClick={() => setFocus({ result: r.id })}>
                  <img src={wbUrl(r, 240)} alt="" />
                  <div style={{ minWidth: 0, flex: 1 }}>
                    <div className="ellipsis fs-12" style={{ fontWeight: 500 }}>{resultTool(r)}</div>
                    <div className="ellipsis fs-12 t3">{r.saved_asset_id ? '已存进素材库' : fmtAgo(r.created_at)}{rs.label !== '实拍' ? ` · ${rs.label}` : ''}</div>
                  </div>
                  {r.saved_asset_id ? <Check className="st ok" /> : null}
                  <button className="tag-x" title="从处理台删掉" onClick={e => { e.stopPropagation(); removeResult(r) }}><X /></button>
                </div>
              )
            })}
        </aside>

        <div className="lib-main">
          <div className="lib-toolbar">
            {curResult ? <>
              <span className="fs-13" style={{ fontWeight: 600 }}>{resultTool(curResult)}</span>
              <span className="t3 fs-12">{resultSrc ? srcName(resultSrc) : curResult.asset ? `${curResult.asset.product_name}${curResult.asset.code ? ' · ' + curResult.asset.code : ''}` : curResult.orig_name}</span>
              <span className="t3 fs-12 mono">{curResult.width}×{curResult.height}</span>
              <span className="spacer" />
              {resultSrc && <div className="segmented">
                <button className={view === 'slide' ? 'active' : ''} onClick={() => setView('slide')}><SplitSquareHorizontal />滑动对比</button>
                <button className={view === 'side' ? 'active' : ''} onClick={() => setView('side')}><Columns2 />并排</button>
              </div>}
            </> : cur ? <>
              <span className="fs-13" style={{ fontWeight: 600 }}>{srcName(cur)}</span>
              <span className="t3 fs-12 mono">{srcSize(cur).join('×')}</span>
              <Pill kind={(sourceName[srcSource(cur)] || sourceName.original).pill} dot={false}>{(sourceName[srcSource(cur)] || sourceName.original).label}</Pill>
            </> : <span className="t3 fs-12">先在左边添加要处理的图</span>}
          </div>
          <div className={'canvas proc-stage' + (over ? ' over' : '')} onDragOver={e => { e.preventDefault(); setOver(true) }} onDragLeave={() => setOver(false)}
            onDrop={e => { e.preventDefault(); setOver(false); upload(Array.from(e.dataTransfer.files)) }}>
            {curResult ? (resultSrc
              ? <Compare beforeUrl={srcUrl(resultSrc, 960)} beforeSize={srcSize(resultSrc)} after={curResult} mode={view} />
              : <div className="proc-frame" style={{ aspectRatio: `${curResult.width || 1} / ${curResult.height || 1}` }}><img src={wbUrl(curResult, 960)} alt="" /></div>)
              : cur ? <Preview url={srcUrl(cur, 960)} size={srcSize(cur)} rotate={tool === 'crop' ? o.rotate : 0} aspect={tool === 'crop' ? o.aspect : 'free'} />
                : <Empty title="还没有图" desc="上传、粘贴（Ctrl+V）、拖进来，或者从素材库选。处理完先放在这里，满意了再保存到素材库或下载。" />}
          </div>
          {curResult && (
            <div className="proc-note stack" style={{ gap: 6 }}>
              {(curResult.op?.warnings || []).map((w, i) => <div key={i} className="status-note">{w}</div>)}
              <div className="row" style={{ gap: 8, flexWrap: 'wrap' }}>
                {curResult.saved_asset_id
                  ? <span className="fs-12 t2"><Check style={{ width: 14, height: 14, verticalAlign: -2 }} /> 已存进素材库 <a className="link" onClick={() => nav('/assets')}>去素材库看</a></span>
                  : <button className="btn btn-secondary btn-sm" onClick={() => setSaving(curResult)}><Save />保存到素材库</button>}
                {can('export.run') && <a className="btn btn-secondary btn-sm" href={`${curResult.url}?download=1`}><Download />下载</a>}
                {curResult.op?.diff && <span className="t3 fs-12">和原图差异 <span className="mono">{curResult.op.diff.phash_distance}</span>{curResult.op.diff.phash_distance > 16 ? '，商品外观可能被改了，请对照原图' : ''}</span>}
              </div>
            </div>
          )}
          {jobIds.length > 0 && (running || lastResults.length > 0) && (
            <div className="bulkbar lib-bulk">
              {running ? <>
                <Loader2 className="spin" style={{ width: 16, height: 16 }} />
                <span className="count">正在{t.label}</span>
                <span className="t3 fs-12 ellipsis">{jobIds.map(id => jobs[id]?.message).filter(Boolean).join(' · ') || '排队中'}</span>
                <div className="progress" style={{ flex: 1, maxWidth: 240 }}><span style={{ width: `${jobs[jobIds[0]]?.progress || 0}%` }} /></div>
              </> : <>
                <span className="count">这次处理了 <span className="mono">{lastResults.length}</span> 张</span>
                <span className="t3 fs-12">还没进素材库</span>
                <div className="actions">
                  {lastResults.some(r => r.asset && !r.saved_asset_id) && <button className="btn btn-secondary btn-sm" onClick={saveAllVersions}><Save />全部存成原图的新版本</button>}
                  {can('export.run') && <button className="btn btn-secondary btn-sm" onClick={() => downloadAll(lastResults.map(r => r.id))}><Download />全部下载</button>}
                </div>
              </>}
            </div>
          )}
        </div>

        <aside className="inspector proc-panel">
          <div className="inspector-body">
            <div className="inspector-section">
              <div className="tool-grid">
                {TOOLS.map(x => (
                  <button key={x.key} className={'tool' + (tool === x.key ? ' active' : '')} disabled={!can(x.perm)} title={can(x.perm) ? x.desc : '没有这项权限'} onClick={() => setTool(x.key)}>
                    <x.icon /><span>{x.label}</span>
                  </button>
                ))}
              </div>
            </div>
            <div className="inspector-section">
              <div className="head"><span className="sub-title">{t.label}</span><span className="t4 fs-12">{t.ai ? '花 AI 额度' : '本地算，不花额度'}</span></div>
              {tool === 'upscale' && <>
                <div className="proc-row"><span className="k">长边</span>
                  <div className="segmented">{[2048, 3000, 4096].map(n => <button key={n} className={o.upscale === n ? 'active' : ''} onClick={() => upd({ upscale: n })}>{n}</button>)}</div>
                </div>
                <div className="t3 fs-12">AI 先把细节重画清楚，再放大到长边 {o.upscale}px。不是无损放大，细节会有变化，处理完对照一下原图。结果算「AI 增强」。</div>
              </>}
              {tool === 'ai' && <>
                <div className="fs-12 t2">快捷需求</div>
                <div className="quick-grid">
                  {quickOps.map(q => {
                    const on = o.quick.includes(q.key)
                    return <button key={q.key} className={'quick' + (on ? ' on' : '') + (q.heavy ? ' heavy' : '')}
                      onClick={() => upd({ quick: on ? o.quick.filter(k => k !== q.key) : [...o.quick, q.key] })}>{on && <Check />}{q.label}</button>
                  })}
                </div>
                <div className="t4 fs-12">前四个是轻度修整，结果算「AI 增强」；后四个会改画面，结果算「AI 改图」。</div>
                <Field label="额外需求" hint="写了就算「AI 改图」。">
                  <textarea className="textarea" rows={4} maxLength={4000} value={o.extra} placeholder="如：背景换成米白亚麻布，保留珍珠原有光泽" onChange={e => upd({ extra: e.target.value })} />
                </Field>
              </>}
              {tool === 'cutout' && <>
                <div className="proc-row"><span className="k">背景</span>
                  <div className="segmented">{([['white', '纯白'], ['transparent', '透明']] as const).map(([k, l]) => <button key={k} className={o.cutout === k ? 'active' : ''} onClick={() => upd({ cutout: k })}>{l}</button>)}</div>
                </div>
                <div className="t3 fs-12">尺寸不变，只换背景。{o.cutout === 'transparent' ? '透明背景存成 PNG。' : ''}</div>
              </>}
              {tool === 'color' && <>
                <label className="filter-opt" style={{ padding: 0 }}><input type="checkbox" className="switch" checked={o.autoWB} onChange={e => upd({ autoWB: e.target.checked })} />自动白平衡和曝光</label>
                {assetSrcs.length > 1 ? (
                  <Field label="统一色调" hint="把这批图的色温和亮度向选中的那张看齐。">
                    <select className="input" value={o.matchTo} onChange={e => upd({ matchTo: Number(e.target.value) })}>
                      <option value={0}>不统一</option>
                      {assetSrcs.map(s => <option key={s.key} value={s.asset!.id}>{srcName(s)}</option>)}
                    </select>
                  </Field>
                ) : <div className="t4 fs-12">从素材库选两张以上的图，可以把色调统一到其中一张。</div>}
              </>}
              {tool === 'crop' && <>
                <div className="proc-row"><span className="k">旋转</span>
                  <div className="segmented">{[0, 90, 180, 270].map(r => <button key={r} className={o.rotate === r ? 'active' : ''} onClick={() => upd({ rotate: r })}>{r ? `${r}°` : '不转'}</button>)}</div>
                </div>
                <div className="proc-row"><span className="k">比例</span>
                  <div className="segmented">{['free', '1:1', '3:4', '4:3', '16:9'].map(r => <button key={r} className={o.aspect === r ? 'active' : ''} onClick={() => upd({ aspect: r })}>{r === 'free' ? '不裁' : r}</button>)}</div>
                </div>
                <div className="t3 fs-12">按比例居中裁，左边预览里暗下去的部分会被裁掉。</div>
              </>}
              {tool === 'resize' && <>
                <div className="proc-row"><span className="k">长边</span>
                  <select className="input" style={{ width: 140 }} value={o.resize} onChange={e => upd({ resize: Number(e.target.value) })}>
                    <option value={0}>保持</option>{[800, 1200, 1600, 2000, 3000].map(n => <option key={n} value={n}>{n}px</option>)}
                  </select>
                </div>
                <div className="proc-row"><span className="k">格式</span>
                  <div className="segmented">{(['jpg', 'png'] as const).map(k => <button key={k} className={o.format === k ? 'active' : ''} onClick={() => upd({ format: k })}>{k.toUpperCase()}</button>)}</div>
                </div>
                <div className="t3 fs-12">输出时去掉 EXIF。JPG 质量 92。</div>
              </>}
            </div>
          </div>
          <div className="inspector-foot stack" style={{ gap: 8 }}>
            {srcs.length > 1 && <div className="segmented block">
              <button className={scope === 'one' ? 'active' : ''} disabled={!cur} onClick={() => setScope('one')}>只处理选中的</button>
              <button className={scope === 'all' ? 'active' : ''} onClick={() => setScope('all')}>全部 {srcs.length} 张</button>
            </div>}
            <div className="row between fs-12"><span className="t3">结果来源</span><Pill kind={(sourceName[predicted] || sourceName.original).pill} dot={false}>{(sourceName[predicted] || sourceName.original).label}</Pill></div>
            {t.ai && targets.length > 0 && <div className="row between fs-12"><span className="t3">AI 调用</span><span className="mono">约 {targets.length} 次</span></div>}
            <button className="btn btn-primary btn-block" disabled={busy || running || !ops || !targets.length || !can(t.perm)} onClick={run}>
              <Wand2 />{running ? '处理中…' : targets.length ? `${t.label} ${targets.length} 张` : t.label}
            </button>
            {!ops && <div className="t4 fs-12">{tool === 'ai' ? '选一个快捷需求或写额外需求。' : tool === 'crop' ? '选旋转角度或裁剪比例。' : '至少开一项。'}</div>}
          </div>
        </aside>
      </div>

      {picking && <AssetPicker onClose={() => setPicking(false)} onPick={list => { setPicking(false); addAssets(list) }} />}
      {saving && <SaveDialog item={saving} onClose={() => setSaving(null)} onSaved={() => { setSaving(null); loadResults() }} />}
    </>
  )
}

// 处理前预览：按选的角度转，按比例画出会被裁掉的部分
function Preview({ url, size, rotate, aspect }: { url: string; size: number[]; rotate: number; aspect: string }) {
  const swap = rotate === 90 || rotate === 270
  const [W, H] = size
  const w = swap ? H : W, h = swap ? W : H
  const r = RATIOS[aspect]
  let box = { w: 100, h: 100 }
  if (r && w && h) box = w / h > r ? { w: (h * r / w) * 100, h: 100 } : { w: 100, h: (w / r / h) * 100 }
  return (
    <div className="proc-frame" style={{ aspectRatio: `${w || 1} / ${h || 1}` }}>
      <img src={url} alt="" style={swap ? { width: `${(h / w) * 100}%`, height: `${(w / h) * 100}%`, transform: `rotate(${rotate}deg)` } : { transform: rotate ? `rotate(${rotate}deg)` : undefined }} />
      {r && <div className="proc-crop" style={{ width: `${box.w}%`, height: `${box.h}%` }} />}
      {rotate > 0 && <span className="proc-badge"><RotateCw />{rotate}°</span>}
    </div>
  )
}

// 处理后对比：滑动（左原图右结果）或并排；形状不同时只能并排
function Compare({ beforeUrl, beforeSize, after, mode }: { beforeUrl: string; beforeSize: number[]; after: WbItem; mode: 'slide' | 'side' }) {
  const [x, setX] = useState(50)
  const as = sourceName[after.source] || sourceName.original
  const differ = Math.abs(beforeSize[0] / (beforeSize[1] || 1) - after.width / (after.height || 1)) > 0.02
  if (mode === 'side' || differ) return (
    <div className="proc-side">
      <figure><img src={beforeUrl} alt="" /><figcaption>原图 · <span className="mono">{beforeSize.join('×')}</span></figcaption></figure>
      <figure><img src={wbUrl(after, 960)} alt="" /><figcaption>结果 · {as.label} · <span className="mono">{after.width}×{after.height}</span></figcaption></figure>
    </div>
  )
  return (
    <div className="proc-frame compare" style={{ aspectRatio: `${after.width || 1} / ${after.height || 1}` }}>
      <img src={wbUrl(after, 960)} alt="" />
      <img src={beforeUrl} alt="" className="before" style={{ clipPath: `inset(0 ${100 - x}% 0 0)` }} />
      <div className="compare-line" style={{ left: `${x}%` }} />
      <span className="proc-badge left">原图</span><span className="proc-badge">结果 · {as.label}</span>
      <input type="range" min={0} max={100} value={x} onChange={e => setX(Number(e.target.value))} aria-label="对比位置" />
    </div>
  )
}

// 从素材库选图
function AssetPicker({ onClose, onPick }: { onClose: () => void; onPick: (list: Asset[]) => void }) {
  const [q, setQ] = useState('')
  const [items, setItems] = useState<Asset[] | null>(null)
  const [sel, setSel] = useState<Map<number, Asset>>(new Map())
  useEffect(() => {
    const t = setTimeout(() => api<Page<Asset>>('/api/assets' + qs({ q, page_size: 60 })).then(r => setItems(r.items)).catch(() => setItems([])), q ? 250 : 0)
    return () => clearTimeout(t)
  }, [q])
  const toggle = (a: Asset) => setSel(m => { const n = new Map(m); n.has(a.id) ? n.delete(a.id) : n.set(a.id, a); return n })
  return (
    <Drawer title="从素材库选图" width={720} onClose={onClose} foot={<>
      <button className="btn btn-secondary" onClick={onClose}>取消</button>
      <button className="btn btn-primary" disabled={!sel.size} onClick={() => onPick([...sel.values()])}>添加 {sel.size} 张</button>
    </>}>
      <label className="field"><Search /><input autoFocus placeholder="搜文件名、标题、标签、款名" value={q} onChange={e => setQ(e.target.value)} /></label>
      {!items ? <Loading /> : items.length === 0 ? <Empty title="没有找到" /> : (
        <div className="lib-grid" style={{ padding: 0 }}>
          {items.map(a => (
            <div key={a.id} className={'lib-item' + (sel.has(a.id) ? ' selected' : '')} onClick={() => toggle(a)}>
              <div className="lib-img">
                <img src={a.thumb_url || imgUrl(a, 240)} alt="" loading="lazy" />
                <input type="checkbox" className="check lib-check" style={{ opacity: 1 }} checked={sel.has(a.id)} readOnly />
              </div>
              <div className="lib-meta">
                <div className="ellipsis fs-12" style={{ fontWeight: 500 }}>{a.code && <span className="mono">{a.code} · </span>}{assetTitle(a)}</div>
                <div className="ellipsis t3 fs-12">{a.product?.name}</div>
              </div>
            </div>
          ))}
        </div>
      )}
    </Drawer>
  )
}

// 保存到素材库：来自素材的默认存成原图的新版本；上传的要选存到哪一款
export function SaveDialog({ item, onClose, onSaved, tryon = false }: { item: WbItem; onClose: () => void; onSaved: () => void; tryon?: boolean }) {
  const toast = useToast()
  const fromAsset = !!item.asset
  const [mode, setMode] = useState<'version' | 'other'>(fromAsset ? 'version' : 'other')
  const [setCurrent, setSetCurrent] = useState(false)
  const [pid, setPid] = useState<number | null>(null)
  const [q, setQ] = useState('')
  const [list, setList] = useState<Product[] | null>(null)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    if (mode !== 'other') return
    const t = setTimeout(() => api<Page<Product>>('/api/products' + qs({ q, page_size: 50 })).then(r => setList(r.items)).catch(() => setList([])), q ? 250 : 0)
    return () => clearTimeout(t)
  }, [q, mode])
  const submit = async () => {
    setBusy(true)
    try {
      await api(`/api/workbench/items/${item.id}/save`, { body: mode === 'version' ? { set_current: setCurrent } : { product_id: pid } })
      toast(mode === 'version' ? '已存成原图的新版本' : '已存进素材库', 'ok'); onSaved()
    } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  return (
    <Drawer title="保存到素材库" width={440} onClose={onClose} foot={<>
      <button className="btn btn-secondary" onClick={onClose}>取消</button>
      <button className="btn btn-primary" disabled={busy || (mode === 'other' && !pid)} onClick={submit}>保存</button>
    </>}>
      {tryon && <div className="help">保存后，这张 AI 穿戴照将按目标商品的权限共享，能查看该商品的同事也能看到它。</div>}
      {fromAsset && (
        <div className="segmented block">
          <button className={mode === 'version' ? 'active' : ''} onClick={() => setMode('version')}>存成原图的新版本</button>
          <button className={mode === 'other' ? 'active' : ''} onClick={() => setMode('other')}>存到别的款</button>
        </div>
      )}
      {mode === 'version' && item.asset ? <>
        <div className="fs-13">存到「{item.asset.product_name}」，和原图放在同一条版本链里，原图不动。</div>
        {item.asset.code && !tryon && <label className="option">
          <input type="checkbox" className="check" checked={setCurrent} onChange={e => setSetCurrent(e.target.checked)} />
          <div><div className="title">设为 {item.asset.code} 的当前用图</div><div className="desc">下次一键出图就用这张。一键出图正在跑时不能改。</div></div>
        </label>}
      </> : <>
        {!fromAsset && <div className="t3 fs-12">上传的图要放到某一款下面，才能在素材库里找到、用来出图。</div>}
        <label className="field"><Search /><input placeholder="搜款名" value={q} onChange={e => setQ(e.target.value)} /></label>
        {!list ? <Loading /> : list.length === 0 ? <Empty title="没有找到" /> : (
          <div className="list">{list.map(p => (
            <label key={p.id} className="list-item" style={{ cursor: 'pointer' }}>
              <input type="radio" className="radio" checked={pid === p.id} onChange={() => setPid(p.id)} />
              {p.cover_url ? <img className="cover" src={p.cover_url} alt="" /> : <span className="cover"><ImageOff /></span>}
              <div style={{ minWidth: 0 }}><div className="title ellipsis">{p.name}</div><div className="meta mono">#{p.id}</div></div>
            </label>
          ))}</div>
        )}
      </>}
    </Drawer>
  )
}
