import { useEffect, useRef, useState, type KeyboardEvent } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { Sparkles, X, RotateCcw, ArrowUp, ChevronRight, Loader2, Check, AlertTriangle, Play, LayoutTemplate, Wand2, PenLine, ClipboardCheck, ExternalLink, type LucideIcon } from 'lucide-react'
import { useAssistant, runAction, type ChatMsg } from '../lib/assistant'
import { useJobs, jobRunning } from '../lib/jobs'
import { useToast, errText } from '../lib/toast'
import type { AssistAction, AssistRef } from '../lib/types'

const LOOK: Record<string, { icon: LucideIcon; tint: string }> = {
  open: { icon: ExternalLink, tint: 'tint-blue' },
  run_listing: { icon: Play, tint: 'tint-purple' },
  render_listing: { icon: LayoutTemplate, tint: 'tint-purple' },
  render_batch: { icon: LayoutTemplate, tint: 'tint-purple' },
  process_assets: { icon: Wand2, tint: 'tint-blue' },
  generate_copy: { icon: PenLine, tint: 'tint-green' },
  review: { icon: ClipboardCheck, tint: 'tint-orange' }
}

const refPath = (r: AssistRef) => r.path || (r.type === 'product' ? `/products/${r.id}` : r.type === 'asset' ? `/assets?q=${r.id}` : '')

// 右侧常驻 AI 助手：知道当前页面和选中对象；动作卡要点确认才执行
export function AssistantPanel() {
  const { messages, busy, send, clear, setOpen } = useAssistant()
  const loc = useLocation()
  const [text, setText] = useState('')
  const body = useRef<HTMLDivElement>(null)
  useEffect(() => { body.current?.scrollTo({ top: body.current.scrollHeight, behavior: 'smooth' }) }, [messages.length, busy])

  const submit = () => { if (text.trim() && !busy) { send(text); setText('') } }
  const onKey = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); submit() }
  }
  const onProduct = /^\/products\/\d+/.test(loc.pathname)
  const suggestions = [
    '今天先做什么？',
    '出图失败的是哪几款，为什么？',
    ...(onProduct ? ['这款还差哪些步骤没做？', '这款的素材里哪些是 AI 改过的？'] : ['这周哪些款还没有繁体文案？'])
  ]

  return (
    <aside className="assist" aria-label="AI 助手">
      <div className="assist-head">
        <span className="assist-mark"><Sparkles /></span>
        <span className="t1" style={{ fontWeight: 600 }}>助手</span>
        <span className="grow" />
        {messages.length > 0 && <button className="btn btn-ghost btn-icon btn-sm" onClick={clear} title="清空对话" aria-label="清空对话"><RotateCcw /></button>}
        <button className="btn btn-ghost btn-icon btn-sm" onClick={() => setOpen(false)} title="收起" aria-label="收起助手"><X /></button>
      </div>
      <div className="assist-body" ref={body}>
        {messages.length === 0 ? (
          <div className="col" style={{ gap: 12 }}>
            <div className="bubble">我能帮你查商品、素材、出图状态和失败原因，也能帮你准备批量处理、重跑出图、写文案这些操作，你确认后才会执行。</div>
            <div className="col" style={{ gap: 6 }}>
              <div className="eyebrow">可以这样问</div>
              {suggestions.map(s => <button key={s} className="suggest" onClick={() => send(s)}><Sparkles />{s}<ChevronRight className="end" /></button>)}
            </div>
          </div>
        ) : messages.map((m, i) => <Message key={i} m={m} />)}
        {busy && <div className="bubble t3 row" style={{ gap: 6 }}><Loader2 className="spin" />正在查…</div>}
      </div>
      <div className="assist-input">
        <textarea rows={2} value={text} placeholder="一句话告诉我要做什么，Enter 发送" onChange={e => setText(e.target.value)} onKeyDown={onKey} maxLength={2000} />
        <div className="row">
          <span className="fs-12 t4">会用掉文本额度</span>
          <span className="grow" />
          <button className="btn btn-primary btn-icon btn-sm" onClick={submit} disabled={!text.trim() || busy} aria-label="发送"><ArrowUp /></button>
        </div>
      </div>
    </aside>
  )
}

function Message({ m }: { m: ChatMsg }) {
  const nav = useNavigate()
  if (m.role === 'user') return <div className="bubble me">{m.content}</div>
  return (
    <div className="col" style={{ gap: 8 }}>
      <div className={'bubble' + (m.error ? ' err' : '')}>{m.error && <AlertTriangle className="err-icon" />}{m.content}</div>
      {!!m.refs?.length && (
        <div className="assist-refs">
          {m.refs.map(r => { const p = refPath(r); return p ? <button key={r.type + r.id} className="chip outline" onClick={() => nav(p)}>{r.label}</button> : null })}
        </div>
      )}
      {m.actions?.map((a, i) => <ActionCard key={i} a={a} />)}
    </div>
  )
}

function ActionCard({ a }: { a: AssistAction }) {
  const nav = useNavigate()
  const toast = useToast()
  const [state, setState] = useState<'idle' | 'busy' | 'done' | 'error'>('idle')
  const [jobId, setJobId] = useState(0)
  const jobs = useJobs(jobId ? [jobId] : [])
  const job = jobId ? jobs[jobId] : null
  const look = LOOK[a.type] || LOOK.open

  if (a.type === 'open') {
    const path = String(a.params.path || '/')
    return <button className="suggest" onClick={() => nav(path)}><ExternalLink />{a.label}<ChevronRight className="end" /></button>
  }
  const go = async () => {
    setState('busy')
    try {
      const j = await runAction(a)
      if (j) setJobId(j.id)
      setState('done')
      toast(j ? `已开始：${a.label}` : `已完成：${a.label}`, 'ok')
    } catch (e) { setState('error'); toast(errText(e), 'error') }
  }
  const status = job ? (jobRunning(job) ? `进行中 ${job.progress || 0}%` : job.status === 'done' ? '已完成' : `失败：${job.message || '原因未知'}`) : state === 'done' ? '已提交' : ''
  return (
    <div className="act-card">
      <div className="row" style={{ alignItems: 'flex-start' }}>
        <span className={'icon-tile sm ' + look.tint}><look.icon /></span>
        <div className="grow" style={{ minWidth: 0 }}>
          <div className="t1" style={{ fontWeight: 500 }}>{a.label}</div>
          <div className="fs-12 t3">{a.summary}</div>
        </div>
      </div>
      <div className="row">
        {a.cost_hint && <span className="fs-12 t3">{a.cost_hint}</span>}
        {status && <span className={'fs-12 ' + (job?.status === 'failed' ? 'danger-text' : 't2')}>{status}</span>}
        <span className="grow" />
        {state === 'done' || state === 'busy'
          ? <button className="btn btn-secondary btn-sm" disabled>{state === 'busy' ? <Loader2 className="spin" /> : <Check />}{state === 'busy' ? '提交中' : '已确认'}</button>
          : <button className="btn btn-primary btn-sm" onClick={go}>{state === 'error' ? '重试' : '确认执行'}</button>}
      </div>
    </div>
  )
}
