import { useEffect, useMemo, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { ArrowLeft, Wand2, Check, AlertTriangle, Loader2, RotateCw, Save, Columns2, SplitSquareHorizontal } from 'lucide-react'
import { api, type Page } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { useJobs, jobRunning } from '../lib/jobs'
import type { Asset, Preset, ProcessOps } from '../lib/types'
import { Topbar, Pill, Loading, Empty } from '../components/ui'
import { sourceName, assetTitle, quickOps, imgUrl } from '../lib/format'

type Opts = {
  rotate: number; aspect: string; autoWB: boolean; matchTo: number; cutout: '' | 'white' | 'transparent'
  upscale: number; quick: string[]; extra: string; resize: number; format: 'jpg' | 'png'; setCurrent: boolean
}
const EMPTY: Opts = { rotate: 0, aspect: 'free', autoWB: false, matchTo: 0, cutout: '', upscale: 0, quick: [], extra: '', resize: 0, format: 'jpg', setCurrent: false }
const RATIOS: Record<string, number> = { '1:1': 1, '3:4': 3 / 4, '4:3': 4 / 3, '16:9': 16 / 9 }

function toOps(o: Opts): ProcessOps {
  const ops: ProcessOps = { format: o.format }
  if (o.rotate) ops.geometry = { rotate: o.rotate }
  if (o.aspect !== 'free') ops.aspect = o.aspect as ProcessOps['aspect']
  if (o.autoWB || o.matchTo) ops.color = { ...(o.autoWB ? { auto_wb: true } : {}), ...(o.matchTo ? { match_to: o.matchTo } : {}) }
  if (o.cutout) ops.cutout = { background: o.cutout }
  if (o.upscale) ops.upscale = { target_long_side: o.upscale }
  if (o.quick.length || o.extra.trim()) ops.ai = { quick: o.quick, extra: o.extra.trim() }
  if (o.resize) ops.resize = { long_side: o.resize }
  return ops
}
function fromOps(ops: ProcessOps = {}, setCurrent = false): Opts {
  return {
    rotate: ops.geometry?.rotate || 0, aspect: ops.aspect || 'free', autoWB: !!ops.color?.auto_wb, matchTo: ops.color?.match_to || 0,
    cutout: ops.cutout?.background || '', upscale: ops.upscale?.target_long_side || 0, quick: ops.ai?.quick || [], extra: ops.ai?.extra || '',
    resize: ops.resize?.long_side || 0, format: ops.format || 'jpg', setCurrent
  }
}
// 和后端的来源规则一致：写了额外需求或用了改图项 → AI 改图；只高清化或轻度项 → AI 增强；否则继承原图
function resultSource(o: Opts) {
  if (o.extra.trim() || o.quick.some(k => quickOps.find(q => q.key === k)?.heavy)) return 'ai_edited'
  if (o.upscale || o.quick.length) return 'ai_enhanced'
  return ''
}

type Result = { asset?: Asset; error?: string }

export function AssetProcess() {
  const { me, can } = useAuth()
  const toast = useToast()
  const nav = useNavigate()
  const [sp] = useSearchParams()
  const ids = useMemo(() => (sp.get('ids') || '').split(',').map(Number).filter(n => n > 0), [sp])
  const [items, setItems] = useState<Asset[] | null>(null)
  const [focus, setFocus] = useState(0)
  const [o, setO] = useState<Opts>(EMPTY)
  const [presets, setPresets] = useState<Preset[]>([])
  const [presetId, setPresetId] = useState(0)
  const [saving, setSaving] = useState<{ name: string; shared: boolean } | null>(null)
  const [jobIds, setJobIds] = useState<number[]>([])
  const [results, setResults] = useState<Record<number, Result>>({})
  const [busy, setBusy] = useState(false)
  const [view, setView] = useState<'slide' | 'side'>('slide')
  const jobs = useJobs(jobIds)

  useEffect(() => {
    Promise.all(ids.map(id => api<{ asset: Asset }>(`/api/assets/${id}`).then(r => r.asset).catch(() => null)))
      .then(list => { const ok = list.filter((a): a is Asset => !!a); setItems(ok); if (ok[0]) setFocus(ok[0].id) })
  }, [ids])
  const loadPresets = () => can('asset.upload') && api<Page<Preset>>('/api/presets').then(r => setPresets(r.items)).catch(() => {})
  useEffect(() => { loadPresets() }, []) // eslint-disable-line react-hooks/exhaustive-deps

  // 任务结束后读结果：每张输入图对应一个新版本或一条失败原因
  useEffect(() => {
    for (const j of Object.values(jobs)) {
      if (jobRunning(j)) continue
      const list = (j.result?.items as { asset_id: number; new_asset_id?: number; error?: string }[] | undefined) || []
      const pending = list.filter(r => !results[r.asset_id])
      if (j.status === 'failed' && list.length === 0) {
        const inJob = ((j as unknown as { params?: { asset_ids?: number[] } }).params?.asset_ids) || []
        const fresh = inJob.filter(id => !results[id])
        if (fresh.length) setResults(r => ({ ...r, ...Object.fromEntries(fresh.map(id => [id, { error: j.message || '处理失败' }])) }))
        continue
      }
      if (!pending.length) continue
      Promise.all(pending.map(r => r.new_asset_id
        ? api<{ asset: Asset }>(`/api/assets/${r.new_asset_id}`).then(x => [r.asset_id, { asset: x.asset }] as const).catch(() => [r.asset_id, { error: '读取新版本失败' }] as const)
        : Promise.resolve([r.asset_id, { error: r.error || '处理失败' }] as const)))
        .then(pairs => setResults(prev => ({ ...prev, ...Object.fromEntries(pairs) })))
    }
  }, [jobs]) // eslint-disable-line react-hooks/exhaustive-deps

  if (!items) return <><Topbar crumbs={['图片库', '处理']} /><Loading /></>
  if (!items.length) return <><Topbar crumbs={['图片库', '处理']} /><Empty title="没有可处理的图" desc="回图片库勾选要处理的图。" /></>

  const cur = items.find(a => a.id === focus) || items[0]
  const res = results[cur.id]
  const running = jobIds.some(id => !jobs[id] || jobRunning(jobs[id]))
  const started = jobIds.length > 0
  const doneCount = Object.values(results).filter(r => r.asset).length
  const failCount = Object.values(results).filter(r => r.error).length
  const ops = toOps(o)
  const needAI = !!(ops.ai || ops.upscale)
  const perm = ops.ai ? 'asset.process' : ops.upscale ? 'asset.upscale' : 'asset.upload'
  const allowed = can(perm)
  const nothing = Object.keys(ops).length === 1 && o.format === 'jpg'
  const src = resultSource(o)
  const calls = ((ops.upscale ? 1 : 0) + (ops.ai ? 1 : 0)) * items.length
  const coded = items.filter(a => a.code).length
  const upd = (p: Partial<Opts>) => { setO(v => ({ ...v, ...p })); setPresetId(0) }

  const submit = async () => {
    const groups = new Map<number, number[]>()
    items.forEach(a => groups.set(a.product_id, [...(groups.get(a.product_id) || []), a.id]))
    setBusy(true)
    const got: number[] = []
    try {
      for (const asset_ids of groups.values()) {
        const r = await api<{ job: { id: number } }>('/api/assets/process', { body: { asset_ids, ops, set_current: o.setCurrent, ...(presetId ? { preset_id: presetId } : {}) } })
        got.push(r.job.id)
      }
      setResults({}); setJobIds(got)
      toast(`已开始处理 ${items.length} 张`, 'ok')
    } catch (e) {
      toast(errText(e), 'error')
      if (got.length) setJobIds(got)
    } finally { setBusy(false) }
  }
  const savePreset = async () => {
    if (!saving?.name.trim()) return
    try {
      const r = await api<{ preset: Preset }>('/api/presets', { body: { name: saving.name.trim(), shared: saving.shared, params: { ops: { ...ops, color: ops.color?.match_to ? (o.autoWB ? { auto_wb: true } : undefined) : ops.color }, set_current: o.setCurrent } } })
      toast('已存为预设', 'ok'); setSaving(null); await loadPresets(); setPresetId(r.preset.id)
    } catch (e) { toast(errText(e), 'error') }
  }
  const usePreset = (id: number) => {
    const p = presets.find(x => x.id === id)
    if (!p) { setPresetId(0); return }
    setO(fromOps(p.params.ops, !!p.params.set_current)); setPresetId(id)
  }

  return (
    <>
      <Topbar crumbs={[<a className="link" onClick={() => nav('/assets')}>图片库</a>, `处理 ${items.length} 张`]}>
        <button className="btn btn-ghost btn-sm" onClick={() => nav(-1)}><ArrowLeft />返回</button>
      </Topbar>
      <div className="lib">
        <aside className="sidebar proc-list">
          <div className="nav-label">要处理的图<span className="end">{items.length}</span></div>
          {items.map(a => {
            const r = results[a.id]
            const st = r?.asset ? 'ok' : r?.error ? 'bad' : started && running ? 'run' : ''
            return (
              <button key={a.id} className={'proc-item' + (a.id === cur.id ? ' active' : '')} onClick={() => setFocus(a.id)}>
                <img src={a.thumb_url || a.url + '?w=240'} alt="" />
                <div style={{ minWidth: 0, flex: 1 }}>
                  <div className="ellipsis fs-12" style={{ fontWeight: 500 }}>{a.code && <span className="mono">{a.code} · </span>}{assetTitle(a)}</div>
                  <div className="ellipsis fs-12 t3">{a.product?.name}</div>
                </div>
                {st === 'ok' && <Check className="st ok" />}
                {st === 'bad' && <AlertTriangle className="st bad" />}
                {st === 'run' && <Loader2 className="st spin" />}
              </button>
            )
          })}
        </aside>

        <div className="lib-main">
          <div className="lib-toolbar">
            <span className="fs-13" style={{ fontWeight: 600 }}>{assetTitle(cur)}</span>
            <span className="t3 fs-12 mono">{cur.width}×{cur.height}</span>
            <span className="spacer" />
            {res?.asset && <div className="segmented">
              <button className={view === 'slide' ? 'active' : ''} onClick={() => setView('slide')}><SplitSquareHorizontal style={{ width: 14, height: 14 }} />滑动对比</button>
              <button className={view === 'side' ? 'active' : ''} onClick={() => setView('side')}><Columns2 style={{ width: 14, height: 14 }} />并排</button>
            </div>}
          </div>
          <div className="canvas proc-stage">
            {res?.asset ? <Compare before={cur} after={res.asset} mode={view} />
              : <Preview a={cur} rotate={o.rotate} aspect={o.aspect} />}
          </div>
          {res?.error && <div className="status-note proc-note">这张没处理成：{res.error}</div>}
          {res?.asset?.op?.warnings?.length ? <div className="proc-note stack" style={{ gap: 4 }}>{res.asset.op.warnings.map((w, i) => <div key={i} className="status-note">{w}</div>)}</div> : null}
          {started && (
            <div className="bulkbar">
              {running ? <>
                <Loader2 className="spin" style={{ width: 16, height: 16 }} />
                <span className="count">正在处理</span>
                <span className="t3 fs-12">{Object.values(jobs).map(j => j.message).filter(Boolean).join(' · ') || '排队中'}</span>
                <div className="progress" style={{ flex: 1, maxWidth: 240 }}><span style={{ width: `${Math.round(Object.values(jobs).reduce((s, j) => s + (j.progress || 0), 0) / Math.max(1, jobIds.length))}%` }} /></div>
              </> : <>
                <span className="count">处理完成</span>
                <span className="t3 fs-12">成功 <span className="mono">{doneCount}</span> 张{failCount ? <>，失败 <span className="mono">{failCount}</span> 张</> : null}，原图没动，新版本已存进图片库</span>
                <div className="actions">
                  <button className="btn btn-secondary btn-sm" onClick={() => nav('/assets')}>回图片库</button>
                  {doneCount > 0 && <button className="btn btn-secondary btn-sm" onClick={() => {
                    const next = Object.values(results).map(r => r.asset?.id).filter(Boolean).join(',')
                    setJobIds([]); setResults({}); nav('/process?ids=' + next)
                  }}>接着处理新版本</button>}
                </div>
              </>}
            </div>
          )}
        </div>

        <aside className="inspector proc-panel">
          <div className="inspector-body">
            {can('asset.upload') && (
              <div className="inspector-section">
                <div className="head"><span className="sub-title">预设</span>
                  {!saving && <button className="btn btn-ghost btn-sm" disabled={nothing} onClick={() => setSaving({ name: '', shared: false })}><Save />存为预设</button>}</div>
                <select className="input" value={presetId} onChange={e => usePreset(Number(e.target.value))}>
                  <option value={0}>不用预设</option>
                  {presets.map(p => <option key={p.id} value={p.id}>{p.name}{p.shared ? '（全店）' : ''}</option>)}
                </select>
                {saving && <div className="stack" style={{ gap: 6 }}>
                  <input className="input" autoFocus placeholder="预设名，如：白底主图标准" value={saving.name} onChange={e => setSaving({ ...saving, name: e.target.value })} onKeyDown={e => e.key === 'Enter' && savePreset()} />
                  <div className="row between">
                    {can('asset.process') ? <label className="filter-opt"><input type="checkbox" className="check" checked={saving.shared} onChange={e => setSaving({ ...saving, shared: e.target.checked })} />全店共享</label> : <span />}
                    <span className="inline"><button className="btn btn-ghost btn-sm" onClick={() => setSaving(null)}>取消</button><button className="btn btn-secondary btn-sm" disabled={!saving.name.trim()} onClick={savePreset}>保存</button></span>
                  </div>
                </div>}
              </div>
            )}

            <div className="inspector-section">
              <div className="head"><span className="sub-title">基础处理</span><span className="t4 fs-12">本地算，不花额度</span></div>
              <div className="proc-row"><span className="k">旋转</span>
                <div className="segmented">{[0, 90, 180, 270].map(r => <button key={r} className={o.rotate === r ? 'active' : ''} onClick={() => upd({ rotate: r })}>{r ? `${r}°` : '不转'}</button>)}</div>
              </div>
              <div className="proc-row"><span className="k">比例</span>
                <div className="segmented">{['free', '1:1', '3:4', '4:3', '16:9'].map(r => <button key={r} className={o.aspect === r ? 'active' : ''} onClick={() => upd({ aspect: r })}>{r === 'free' ? '原比例' : r}</button>)}</div>
              </div>
              <div className="proc-row"><span className="k">校色</span>
                <label className="filter-opt"><input type="checkbox" className="switch" checked={o.autoWB} onChange={e => upd({ autoWB: e.target.checked })} />自动白平衡</label>
              </div>
              {items.length > 1 && <div className="proc-row"><span className="k" />
                {o.matchTo ? <span className="fs-12 t2">色调向 <b>{assetTitle(items.find(a => a.id === o.matchTo) || { id: o.matchTo, orig_name: '' })}</b> 看齐 <a className="link" onClick={() => upd({ matchTo: 0 })}>取消</a></span>
                  : <a className="link fs-12" onClick={() => upd({ matchTo: cur.id })}>以当前这张为准统一色调</a>}
              </div>}
              <div className="proc-row"><span className="k">抠图</span>
                <div className="segmented">{([['', '不抠'], ['white', '白底'], ['transparent', '透明']] as const).map(([k, t]) => (
                  <button key={k} className={o.cutout === k ? 'active' : ''} onClick={() => upd({ cutout: k, ...(k === 'transparent' ? { format: 'png' as const } : {}) })}>{t}</button>
                ))}</div>
              </div>
            </div>

            <div className="inspector-section">
              <div className="head"><span className="sub-title">AI 处理</span><span className="t4 fs-12">花额度，结果会标记来源</span></div>
              <div className="proc-row"><span className="k">高清化</span>
                <div className="segmented" title={can('asset.upscale') ? '' : '没有高清化权限'}>{[0, 2048, 3000, 4096].map(n => (
                  <button key={n} disabled={!!n && !can('asset.upscale')} className={o.upscale === n ? 'active' : ''} onClick={() => upd({ upscale: n })}>{n ? `${n}` : '不用'}</button>
                ))}</div>
              </div>
              {o.upscale > 0 && <div className="t3 fs-12">AI 重绘后再放大到长边 {o.upscale}px，不是无损放大，细节会有变化。</div>}
              {can('asset.process') ? <>
                <div className="fs-12 t2">快捷需求</div>
                <div className="quick-grid">
                  {quickOps.map(q => {
                    const on = o.quick.includes(q.key)
                    return <button key={q.key} className={'quick' + (on ? ' on' : '') + (q.heavy ? ' heavy' : '')}
                      onClick={() => upd({ quick: on ? o.quick.filter(k => k !== q.key) : [...o.quick, q.key] })}>{on && <Check />}{q.label}</button>
                  })}
                </div>
                <div className="t4 fs-12">前四个是轻度修整，结果算「AI 增强」；后四个会改画面，结果算「AI 改图」。</div>
                <label className="form-field"><span className="label">额外需求</span>
                  <textarea className="textarea" rows={3} maxLength={4000} value={o.extra} placeholder="如：背景换成米白亚麻布，保留珍珠原有光泽。写了就算「AI 改图」。" onChange={e => upd({ extra: e.target.value })} />
                </label>
              </> : <div className="t3 fs-12">AI 改图需要「图片处理」权限，找管理员开。</div>}
            </div>

            <div className="inspector-section">
              <div className="head"><span className="sub-title">输出</span></div>
              <div className="proc-row"><span className="k">长边</span>
                <select className="input" style={{ width: 140 }} value={o.resize} onChange={e => upd({ resize: Number(e.target.value) })}>
                  <option value={0}>保持</option>{[1200, 1600, 2000, 3000].map(n => <option key={n} value={n}>{n}px</option>)}
                </select>
              </div>
              <div className="proc-row"><span className="k">格式</span>
                <div className="segmented">{(['jpg', 'png'] as const).map(k => <button key={k} className={o.format === k ? 'active' : ''} onClick={() => upd({ format: k })}>{k.toUpperCase()}</button>)}</div>
              </div>
              {o.cutout === 'transparent' && o.format === 'jpg' && <div className="status-note fs-12">JPG 不支持透明，会合成白底。</div>}
              {coded > 0 && <label className="option">
                <input type="checkbox" className="check" checked={o.setCurrent} onChange={e => upd({ setCurrent: e.target.checked })} />
                <div><div className="title">处理完设为当前用图</div><div className="desc">{coded} 张有拍摄编号，编号会转给新版本，下次一键出图就用新的。</div></div>
              </label>}
            </div>
          </div>
          <div className="inspector-foot stack" style={{ gap: 8 }}>
            <div className="row between fs-12">
              <span className="t3">结果来源</span>
              {src ? <Pill kind={sourceName[src].pill} dot={false}>{sourceName[src].label}</Pill> : <span className="t2">和原图一样</span>}
            </div>
            {needAI && <div className="row between fs-12"><span className="t3">AI 调用</span><span className="mono">约 {calls} 次</span></div>}
            {src === 'ai_edited' && <div className="t3 fs-12">导出时默认加「AI 生成」角标。</div>}
            <button className="btn btn-primary btn-block" disabled={busy || running || nothing || !allowed} onClick={submit}>
              <Wand2 />{running ? '处理中…' : `处理 ${items.length} 张`}
            </button>
            {!allowed && <div className="form-err">你没有这项处理的权限</div>}
            {me && nothing && <div className="t4 fs-12">先选至少一项处理。</div>}
          </div>
        </aside>
      </div>
    </>
  )
}

// 处理前预览：按选的旋转角度转，按比例画出会被裁掉的部分
function Preview({ a, rotate, aspect }: { a: Asset; rotate: number; aspect: string }) {
  const swap = rotate === 90 || rotate === 270
  const w = swap ? a.height : a.width, h = swap ? a.width : a.height
  const r = RATIOS[aspect]
  let box = { w: 100, h: 100 }
  if (r && w && h) box = w / h > r ? { w: (h * r / w) * 100, h: 100 } : { w: 100, h: (w / r / h) * 100 }
  return (
    <div className="proc-frame" style={{ aspectRatio: `${w || 1} / ${h || 1}` }}>
      <img src={imgUrl(a, 960)} alt="" style={swap ? { width: `${(h / w) * 100}%`, height: `${(w / h) * 100}%`, transform: `rotate(${rotate}deg)` } : { transform: rotate ? `rotate(${rotate}deg)` : undefined }} />
      {r && <div className="proc-crop" style={{ width: `${box.w}%`, height: `${box.h}%` }} />}
      {rotate > 0 && <span className="proc-badge"><RotateCw />{rotate}°</span>}
    </div>
  )
}

// 处理后对比：滑动（左边原图，右边新版本）或并排
function Compare({ before, after, mode }: { before: Asset; after: Asset; mode: 'slide' | 'side' }) {
  const [x, setX] = useState(50)
  const as = sourceName[after.effective_source || after.source] || sourceName.original
  // 旋转或改了比例后两张形状不同，叠在一起滑动会错位，只能并排看
  const differ = Math.abs(before.width / (before.height || 1) - after.width / (after.height || 1)) > 0.02
  if (mode === 'side' || differ) return (
    <div className="proc-side">
      <figure><img src={imgUrl(before, 960)} alt="" /><figcaption>原图 · <span className="mono">{before.width}×{before.height}</span></figcaption></figure>
      <figure><img src={imgUrl(after, 960)} alt="" /><figcaption>新版本 · {as.label} · <span className="mono">{after.width}×{after.height}</span></figcaption></figure>
    </div>
  )
  return (
    <div className="proc-frame compare" style={{ aspectRatio: `${after.width || 1} / ${after.height || 1}` }}>
      <img src={imgUrl(after, 960)} alt="" />
      <img src={imgUrl(before, 960)} alt="" className="before" style={{ clipPath: `inset(0 ${100 - x}% 0 0)` }} />
      <div className="compare-line" style={{ left: `${x}%` }} />
      <span className="proc-badge left">原图</span><span className="proc-badge">新版本 · {as.label}</span>
      <input type="range" min={0} max={100} value={x} onChange={e => setX(Number(e.target.value))} aria-label="对比位置" />
    </div>
  )
}
