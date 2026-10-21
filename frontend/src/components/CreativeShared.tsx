import { useEffect, useRef, useState } from 'react'
import { Check, RefreshCw, Ban, Type, AlertTriangle } from 'lucide-react'
import { Pill } from './ui'
import { useLightbox } from './Lightbox'
import { useToast, errText } from '../lib/toast'
import { CHECK_NAME, MODE, WEAR, creativeApi, criticalFail, itemState, minScore, slotName, type CreativeItem, type Review, type Verdict } from '../lib/creative'

// 一张生成图的缩略卡：位置、版本、结论、分数，点开看详情
export function ItemCard({ it, onOpen, active, idle }: { it: CreativeItem; onOpen: () => void; active?: boolean; idle?: boolean }) {
  const st = itemState(it)
  const score = minScore(it)
  return (
    <button type="button" className={'cr-card' + (active ? ' active' : '') + (it.superseded ? ' old' : '')} onClick={onOpen}>
      <div className="cr-img">
        {it.output_url ? <img src={it.output_url} alt="" loading="lazy" /> : <span className="t4 fs-12">{it.error ? '生成失败' : idle ? '未生成' : '生成中…'}</span>}
        {it.variant && <span className="cr-variant">{it.variant}</span>}
        {criticalFail(it) && <span className="cr-flag" title="参数文字比对没过，改字前不能导出"><AlertTriangle />字不对</span>}
      </div>
      <div className="cr-meta">
        <span className="cell-title">{slotName(it.slot)}{it.seq > 1 ? ` · ${it.seq}` : ''}</span>
        <span className="grow" />
        {score !== null && <span className={'mono fs-12 ' + (score >= 90 ? 't2' : score >= 60 ? 'cr-mid' : 'cr-low')}>{score}</span>}
      </div>
      <div className="cr-meta"><Pill kind={st.pill}>{st.label}</Pill>{it.attempt > 1 && <span className="t4 fs-12">第 {it.attempt} 次</span>}</div>
    </button>
  )
}

// 原图和生成图左右对比；生成图上框出复核找到的问题
export function CompareView({ it }: { it: CreativeItem }) {
  const lightbox = useLightbox()
  const [show, setShow] = useState(true)
  const issues = [...(it.review_a?.issues || []), ...(it.review_b?.issues || [])]
  const src = it.source_urls || []
  return (
    <div className="cr-compare">
      <figure>
        <figcaption>原图{src.length > 1 ? ` · ${src.length} 张` : ''}</figcaption>
        {src.length ? <div className="cr-srcs">{src.map((u, i) => <button key={i} type="button" className="zoomable" onClick={() => lightbox(src.map(s => ({ src: s, title: '原图', download: '' })), i)}><img src={u} alt="" /></button>)}</div>
          : <div className="cr-none t4 fs-12">{it.product_mode === 'none' ? '这一屏不放商品' : '没有原图'}</div>}
      </figure>
      <figure>
        <figcaption className="row between">生成图{issues.length > 0 && <label className="row fs-12 t3"><input type="checkbox" className="check" checked={show} onChange={e => setShow(e.target.checked)} />框出问题</label>}</figcaption>
        <div className="cr-out">
          {it.output_url ? <img src={it.output_url} alt="" onClick={() => lightbox([{ src: it.output_url!, title: slotName(it.slot), download: '' }], 0)} /> : <div className="cr-none t4 fs-12">{it.error || '还没有生成'}</div>}
          {show && issues.map((x, i) => (
            <span key={i} className="cr-box" style={{ left: `${x.box[0] * 100}%`, top: `${x.box[1] * 100}%`, width: `${x.box[2] * 100}%`, height: `${x.box[3] * 100}%` }}><em>{i + 1}</em></span>
          ))}
        </div>
      </figure>
    </div>
  )
}

const CHECK_PILL: Record<string, [string, string]> = { pass: ['pill-success', '通过'], fail: ['pill-danger', '不通过'], unsure: ['pill-warning', '不确定'], na: ['pill-neutral', '不适用'] }
const cell = (v?: string) => { const [k, l] = CHECK_PILL[v || ''] || ['pill-neutral', '—']; return <Pill kind={k} dot={false}>{l}</Pill> }

// 两次 AI 复核、硬检查和分流原因
export function ReviewDetail({ it }: { it: CreativeItem }) {
  const a = it.review_a, b = it.review_b
  const issues = [...(a?.issues || []), ...(b?.issues || [])]
  const h = it.hard
  return (
    <div className="stack" style={{ gap: 12 }}>
      <div className="fs-12 t3 inline">
        <span>{MODE[it.product_mode]}</span>{it.wear_type && <><span className="sep" />{WEAR[it.wear_type]}</>}
        <span className="sep" /><span className="mono">{it.template_key} v{it.template_version}</span><span className="sep" />第 {it.attempt} 次生成
      </div>
      {it.route_reasons.length > 0 && <div className="inline">{it.route_reasons.map(r => <span key={r} className="tag tag-blue">{r}</span>)}</div>}
      {it.error && <div className="cr-warn">{it.error}</div>}
      {!a && !b ? <div className="fs-12 t3">还没有 AI 复核结果</div> : <table className="table cr-checks">
        <thead><tr><th>检查</th><th>第 1 次</th><th>第 2 次（换顺序）</th></tr></thead>
        <tbody>
          {Object.keys(CHECK_NAME).map(k => <tr key={k}><td>{CHECK_NAME[k]}</td><td>{cell(a?.checks[k as keyof Review['checks']])}</td><td>{cell(b?.checks[k as keyof Review['checks']])}</td></tr>)}
          <tr><td>总分</td><td className="mono">{a?.score ?? '—'}</td><td className="mono">{b?.score ?? '—'}</td></tr>
          {(a?.size_mm.length || b?.size_mm.length) ? <tr><td>估算尺寸</td><td className="mono">{a?.size_mm.join(' / ') || '—'} mm</td><td className="mono">{b?.size_mm.join(' / ') || '—'} mm</td></tr> : null}
        </tbody>
      </table>}
      {issues.length > 0 && <ol className="cr-issues">{issues.map((x, i) => <li key={i}><span className="tag tag-gray">{CHECK_NAME[x.check] || x.check}</span>{x.reason}</li>)}</ol>}
      {h && (
        <div className="stack" style={{ gap: 6 }}>
          <div className="sub-title">程序硬检查</div>
          <div className="inline fs-12">
            {h.pixel && <span>像素锁定 {h.pixel.pass ? <Pill kind="pill-success" dot={false}>差异 {h.pixel.max_diff}</Pill> : <Pill kind="pill-danger" dot={false}>差异 {h.pixel.max_diff}</Pill>}</span>}
            {h.format && <span>尺寸格式 {h.format.pass ? <Pill kind="pill-success" dot={false}>通过</Pill> : <Pill kind="pill-danger" dot={false}>不对</Pill>}</span>}
            {(h.banned || []).length > 0 && <span>违禁词 <Pill kind="pill-danger" dot={false}>{h.banned!.join('、')}</Pill></span>}
          </div>
          {h.error && <div className="cr-warn">硬检查出错：{h.error}</div>}
        </div>
      )}
    </div>
  )
}

// 图上的字：应写 / 读出 / 结果，可以直接改字
export function TextRows({ it, canEdit, onDone }: { it: CreativeItem; canEdit: boolean; onDone: () => void }) {
  const toast = useToast()
  const [edit, setEdit] = useState<string | null>(null)
  const [val, setVal] = useState('')
  const [busy, setBusy] = useState(false)
  const rows = it.texts.map(t => ({ ...t, r: it.hard?.text?.find(x => x.key === t.key) }))
  if (!rows.length) return null
  const save = async (key: string) => {
    setBusy(true)
    try { await creativeApi.retext(it.id, key, val.trim()); toast('已开始改字，改完会重新核对', 'ok'); setEdit(null); onDone() } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  return (
    <div className="stack" style={{ gap: 6 }}>
      <div className="sub-title">图上的字</div>
      {rows.map(t => (
        <div key={t.key} className="cr-text">
          <div className="grow stack" style={{ gap: 2, minWidth: 0 }}>
            <div className="inline"><span className="mono fs-12 t3">{t.key}</span>{(t.critical || t.r?.critical) && <span className="tag tag-red">参数</span>}
              {t.r ? (t.r.pass ? <Pill kind="pill-success" dot={false}>一致</Pill> : t.r.warn ? <Pill kind="pill-warning" dot={false}>不一致（装饰字）</Pill> : <Pill kind="pill-danger" dot={false}>不一致，不能导出</Pill>) : <Pill kind="pill-neutral" dot={false}>未核对</Pill>}
            </div>
            <div className="fs-13">应写：{t.text}</div>
            {t.r && !t.r.pass && <div className="fs-12 t3">图上：{t.r.found || '（没读到）'}</div>}
            {edit === t.key && (
              <div className="row" style={{ gap: 6 }}>
                <input className="input" autoFocus value={val} onChange={e => setVal(e.target.value)} onKeyDown={e => { if (e.key === 'Enter' && val.trim()) save(t.key); if (e.key === 'Escape') setEdit(null) }} />
                <button className="btn btn-primary btn-sm" disabled={busy || !val.trim()} onClick={() => save(t.key)}>改</button>
                <button className="btn btn-ghost btn-sm" onClick={() => setEdit(null)}>取消</button>
              </div>
            )}
          </div>
          {canEdit && edit !== t.key && <button className="btn btn-ghost btn-sm" onClick={() => { setEdit(t.key); setVal(t.text) }}><Type />改字</button>}
        </div>
      ))}
    </div>
  )
}

// 人工判断：通过 / 重画（必须写原因）/ 弃用。hotkeys 开着时键盘 1、2、3（焦点在输入框里不算）
export function JudgeBar({ it, onDone, hotkeys }: { it: CreativeItem; onDone: () => void; hotkeys?: boolean }) {
  const toast = useToast()
  const noteRef = useRef<HTMLInputElement>(null)
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)
  const keyRef = useRef<(e: KeyboardEvent) => void>(() => {})
  keyRef.current = e => {
    const el = e.target as HTMLElement
    if (busy || e.ctrlKey || e.metaKey || e.altKey || /INPUT|TEXTAREA|SELECT/.test(el.tagName) || el.isContentEditable) return
    const v = ({ '1': 'pass', '2': 'redraw', '3': 'discard' } as Record<string, Verdict>)[e.key]
    if (v) { e.preventDefault(); judge(v) }
  }
  useEffect(() => {
    if (!hotkeys) return
    const h = (e: KeyboardEvent) => keyRef.current(e)
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [hotkeys])
  const judge = async (v: Verdict) => {
    if (v === 'redraw' && !note.trim()) { toast('写一句为什么要重画', 'error'); noteRef?.current?.focus(); return }
    if (v === 'pass' && criticalFail(it)) { toast('参数文字没核对通过，先改字再通过', 'error'); return }
    setBusy(true)
    try { await creativeApi.judge(it.id, v, note.trim()); setNote(''); onDone() } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  return (
    <div className="stack" style={{ gap: 8 }}>
      <input ref={noteRef} className="input" placeholder="备注；要求重画时写清楚哪里不对" value={note} onChange={e => setNote(e.target.value)} />
      <div className="row" style={{ gap: 8 }}>
        <button className="btn btn-primary grow" disabled={busy} onClick={() => judge('pass')}><Check />通过<span className="kbd">1</span></button>
        <button className="btn btn-secondary grow" disabled={busy} onClick={() => judge('redraw')}><RefreshCw />重画<span className="kbd">2</span></button>
        <button className="btn btn-secondary grow" disabled={busy} onClick={() => judge('discard')}><Ban />弃用<span className="kbd">3</span></button>
      </div>
    </div>
  )
}
