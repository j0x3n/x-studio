import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'
import { Calendar, CheckSquare, Plus, X, ExternalLink, Send } from 'lucide-react'
import { api, qs } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import type { Assignee, Task, TaskComment, TaskEvent, TaskPriority, TaskStatus } from '../lib/types'
import { taskStatus, taskPriority, OPEN, dueText, toLocalInput, fromLocalInput, eventText } from '../lib/tasks'
import { fmtAgo, fmtDate, initials } from '../lib/format'
import { Drawer, Field, Loading, Pill } from './ui'

type Target = { type: string; id: number; label?: string }
type Detail = Task & { comments: TaskComment[]; events: TaskEvent[] }

// 可以派活的人；传了对象就只要看得到它的人
export function useAssignees(target?: Target | null) {
  const [list, setList] = useState<Assignee[]>([])
  const key = target ? `${target.type}:${target.id}` : ''
  useEffect(() => {
    api<{ items: Assignee[] } | Assignee[]>('/api/tasks/assignees' + qs({ target_type: target?.type, target_id: target?.id }))
      .then(r => setList(Array.isArray(r) ? r : r.items || [])).catch(() => setList([]))
  }, [key]) // eslint-disable-line react-hooks/exhaustive-deps
  return list
}

// 谁能改什么（约定第 4 节）；后端会再查一遍，这里只用来决定显示成可编辑还是只读
export function useTaskRights(t: Task | null) {
  const { me, can, isAdmin } = useAuth()
  const uid = me?.user.id
  const manage = isAdmin || can('task.manage')
  const edit = !!t && (manage || t.created_by?.id === uid)
  const status = !!t && (edit || t.assignee?.id === uid)
  return { edit, status, manage }
}

export function StatusPill({ s }: { s: TaskStatus }) {
  const v = taskStatus[s] || { label: s, pill: 'pill-neutral' }
  return <Pill kind={v.pill}>{v.label}</Pill>
}
export function PriorityTag({ p, quiet }: { p: TaskPriority; quiet?: boolean }) {
  if (quiet && (p === 'normal' || p === 'low')) return null
  const cls = p === 'urgent' ? 'tag-red' : p === 'high' ? 'tag-orange' : 'tag-gray'
  return <span className={'tag ' + cls}>{taskPriority[p]?.label || p}</span>
}
export function Due({ t }: { t: Task }) {
  if (!t.due_at) return <span className="t4">—</span>
  const late = t.overdue && OPEN.includes(t.status)
  return <span className={'cell-date' + (late ? ' late' : '')} title={fmtDate(t.due_at, true)}><Calendar />{dueText(t.due_at)}{late && '·逾期'}</span>
}
export function Person({ p, empty = '未派' }: { p: { id: number; name: string } | null | undefined; empty?: string }) {
  if (!p) return <span className="t4">{empty}</span>
  return <span className="cell-person"><span className="avatar xs c2">{initials(p.name)}</span><span className="ellipsis">{p.name}</span></span>
}

// 首页和商品详情里的简短列表
export function TaskMiniList({ items, onOpen, empty }: { items: Task[]; onOpen: (t: Task) => void; empty: ReactNode }) {
  if (!items.length) return <>{empty}</>
  return (
    <div className="next-list">
      {items.map(t => (
        <button key={t.id} className={'next-item task-mini' + (t.overdue && OPEN.includes(t.status) ? ' danger' : '')} onClick={() => onOpen(t)}>
          <div className="grow" style={{ minWidth: 0 }}>
            <div className="title ellipsis">{t.title}</div>
            <div className="meta ellipsis">{[t.target?.label, t.assignee ? t.assignee.name : '未派', t.checklist.length ? `清单 ${t.checklist_done}/${t.checklist.length}` : ''].filter(Boolean).join(' · ')}</div>
          </div>
          <PriorityTag p={t.priority} quiet />
          <Due t={t} />
          <StatusPill s={t.status} />
        </button>
      ))}
    </div>
  )
}

// ---------- 新建 ----------
export function NewTaskDrawer({ target, initial, onClose, onCreated }: { target?: Target | null; initial?: { title?: string; body?: string }; onClose: () => void; onCreated: (t: Task) => void }) {
  const { me, can, isAdmin } = useAuth()
  const toast = useToast()
  const manage = isAdmin || can('task.manage')
  const people = useAssignees(target)
  const [f, setF] = useState({ title: initial?.title || '', body: initial?.body || '', priority: 'normal' as TaskPriority, assignee_id: String(me?.user.id || ''), due: '', checklist: '' })
  const [busy, setBusy] = useState(false)
  const set = (k: keyof typeof f, v: string) => setF(x => ({ ...x, [k]: v }))
  const save = async () => {
    if (!f.title.trim()) return toast('写一下任务标题', 'error')
    setBusy(true)
    try {
      const body: Record<string, unknown> = {
        title: f.title.trim(), body: f.body, priority: f.priority,
        assignee_id: f.assignee_id ? Number(f.assignee_id) : 0, // 新建时 null 表示派给自己，0 才是不派
        due_at: fromLocalInput(f.due) || null,
        checklist: f.checklist.split('\n').map(s => s.trim()).filter(Boolean).map(text => ({ text, done: false }))
      }
      if (target) { body.target_type = target.type; body.target_id = target.id }
      const r = await api<{ task: Task }>('/api/tasks', { body })
      toast('任务已建好', 'ok')
      onCreated(r.task)
    } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  return (
    <Drawer title="新建任务" width={480} onClose={onClose} foot={<><span className="grow" /><button className="btn btn-secondary" onClick={onClose}>取消</button><button className="btn btn-primary" disabled={busy} onClick={save}>建好</button></>}>
      {target && <div className="chip outline" style={{ alignSelf: 'flex-start' }}>挂在：{target.label || `${target.type} #${target.id}`}</div>}
      <Field label="标题" req><input className="input" maxLength={100} autoFocus value={f.title} onChange={e => set('title', e.target.value)} placeholder="例如：修图：珠蝶" /></Field>
      <div className="form-row">
        <Field label="负责人" hint={manage ? undefined : '没有派活权限，只能建给自己'}>
          <select className="input" value={f.assignee_id} disabled={!manage} onChange={e => set('assignee_id', e.target.value)}>
            <option value="">先不派</option>
            {!people.some(p => p.id === me?.user.id) && me && <option value={me.user.id}>{me.user.display_name || me.user.username}（我）</option>}
            {people.map(p => <option key={p.id} value={p.id}>{p.name}{p.id === me?.user.id ? '（我）' : ''} · {p.role_name} · {p.open_count} 件</option>)}
          </select>
        </Field>
        <Field label="优先级">
          <select className="input" value={f.priority} onChange={e => set('priority', e.target.value)}>
            {Object.entries(taskPriority).map(([k, v]) => <option key={k} value={k}>{v.label}</option>)}
          </select>
        </Field>
      </div>
      <Field label="截止时间"><input className="input" type="datetime-local" value={f.due} onChange={e => set('due', e.target.value)} /></Field>
      <Field label="说明"><textarea className="textarea" rows={4} maxLength={5000} value={f.body} onChange={e => set('body', e.target.value)} /></Field>
      <Field label="检查清单" hint="一行一项，可以不填"><textarea className="textarea" rows={3} value={f.checklist} onChange={e => set('checklist', e.target.value)} placeholder={'调色\n去灰尘'} /></Field>
    </Drawer>
  )
}

// ---------- 详情 ----------
export function TaskDrawer({ id, onClose, onChanged }: { id: number; onClose: () => void; onChanged?: () => void }) {
  const nav = useNavigate()
  const toast = useToast()
  const { me } = useAuth()
  const [d, setD] = useState<Detail | null>(null)
  const [tab, setTab] = useState<'comments' | 'events'>('comments')
  const closeRef = useRef(onClose)
  closeRef.current = onClose
  const load = useCallback(() => {
    api<Detail | { task: Task; comments: TaskComment[]; events: TaskEvent[] }>(`/api/tasks/${id}`)
      .then(r => setD('task' in r ? { ...r.task, comments: r.comments || [], events: r.events || [] } : { ...r, comments: r.comments || [], events: r.events || [] }))
      .catch(e => { toast(errText(e), 'error'); closeRef.current() })
  }, [id, toast])
  useEffect(() => { load() }, [load])
  const rights = useTaskRights(d)
  const target = d?.target ? { type: d.target.type, id: d.target.id, label: d.target.label } : null
  const people = useAssignees(target)

  const patch = async (body: Record<string, unknown>) => {
    try { await api(`/api/tasks/${id}`, { method: 'PATCH', body }); load(); onChanged?.() } catch (e) { toast(errText(e), 'error'); load() }
  }
  if (!d) return <Drawer title="任务" width={560} onClose={onClose}><Loading /></Drawer>
  const ro = !rights.edit
  const watcherIds = d.watchers.map(w => w.id)

  return (
    <Drawer width={560} onClose={onClose} title={<span className="inline"><span className="mono t3">#{d.id}</span><StatusPill s={d.status} />{d.rule_id ? <span className="tag tag-gray">自动派活</span> : null}</span>}>
      <TitleEdit value={d.title} ro={ro} onSave={v => patch({ title: v })} />
      <div className="task-props">
        <span className="k">状态</span>
        <div className="segmented">{(Object.keys(taskStatus) as TaskStatus[]).map(s => (
          <button key={s} className={d.status === s ? 'active' : ''} disabled={!rights.status} onClick={() => d.status !== s && patch({ status: s })}>{taskStatus[s].label}</button>
        ))}</div>
        <span className="k">负责人</span>
        {ro ? <Person p={d.assignee} /> : (
          <select className="input" style={{ width: 220 }} value={d.assignee?.id ?? ''} onChange={e => patch({ assignee_id: e.target.value ? Number(e.target.value) : null })}>
            <option value="">先不派</option>
            {d.assignee && !people.some(p => p.id === d.assignee!.id) && <option value={d.assignee.id}>{d.assignee.name}</option>}
            {people.map(p => <option key={p.id} value={p.id}>{p.name} · {p.open_count} 件未完成</option>)}
          </select>
        )}
        <span className="k">优先级</span>
        {ro ? <PriorityTag p={d.priority} /> : (
          <select className="input" style={{ width: 120 }} value={d.priority} onChange={e => patch({ priority: e.target.value })}>
            {Object.entries(taskPriority).map(([k, v]) => <option key={k} value={k}>{v.label}</option>)}
          </select>
        )}
        <span className="k">截止</span>
        {ro ? <Due t={d} /> : (
          <span className="inline">
            <input className="input" type="datetime-local" style={{ width: 200 }} defaultValue={toLocalInput(d.due_at)} key={d.due_at} onBlur={e => { const v = fromLocalInput(e.target.value); if (v !== (d.due_at ? new Date(d.due_at).toISOString() : '')) patch({ due_at: v || null }) }} />
            {d.overdue && OPEN.includes(d.status) && <span className="pill pill-danger">已逾期</span>}
          </span>
        )}
        <span className="k">挂在</span>
        {!d.target ? <span className="t4">没挂对象</span>
          : d.target.path ? <a className="link inline" onClick={() => { onClose(); nav(d.target!.path) }}>{d.target.label}<ExternalLink size={13} /></a>
          : <span className="t3">{d.target.label}</span>}
        <span className="k">协作人</span>
        <Watchers list={d.watchers} people={people} ro={ro} onChange={ids => patch({ watcher_ids: ids })} current={watcherIds} />
        <span className="k">创建</span>
        <span className="t3">{d.created_by?.name || '系统'} · {fmtDate(d.created_at, true)}{d.reopen_count > 0 && <> · 返工 <span className="mono">{d.reopen_count}</span> 次</>}</span>
      </div>

      <BodyEdit value={d.body} ro={ro} onSave={v => patch({ body: v })} />
      <Checklist items={d.checklist} canTick={rights.status} canEdit={rights.edit} onChange={c => patch({ checklist: c })} />

      <div className="tabs">
        <button className={'tab' + (tab === 'comments' ? ' active' : '')} onClick={() => setTab('comments')}>评论<span className="count">{d.comments.length}</span></button>
        <button className={'tab' + (tab === 'events' ? ' active' : '')} onClick={() => setTab('events')}>时间线<span className="count">{d.events.length}</span></button>
      </div>
      {tab === 'comments' ? (
        <>
          <div className="task-comments">
            {d.comments.length === 0 && <div className="t4">还没有评论</div>}
            {d.comments.map(c => {
              const who = c.user?.name || c.user_name || `#${c.user_id}`
              return (
                <div key={c.id} className="task-comment">
                  <span className="avatar sm c1">{initials(who)}</span>
                  <div className="grow">
                    <div className="row"><b className="t1">{who}</b><span className="t4 fs-12">{fmtAgo(c.created_at)}</span></div>
                    <div className="task-comment-body">{highlight(c.body)}</div>
                  </div>
                </div>
              )
            })}
          </div>
          <CommentBox taskId={d.id} people={people} meId={me?.user.id} onSent={() => { load(); onChanged?.() }} />
        </>
      ) : (
        <div className="task-events">
          {d.events.length === 0 && <div className="t4">还没有变更记录</div>}
          {d.events.map(e => (
            <div key={e.id} className="task-event">
              <span className="dot" />
              <span className="grow"><b className="t2">{e.user?.name || e.user_name || (e.user_id ? `#${e.user_id}` : '系统')}</b> {eventText(e.kind, e.detail)}</span>
              <span className="t4 fs-12" title={fmtDate(e.created_at, true)}>{fmtAgo(e.created_at)}</span>
            </div>
          ))}
        </div>
      )}
    </Drawer>
  )
}

function TitleEdit({ value, ro, onSave }: { value: string; ro: boolean; onSave: (v: string) => void }) {
  const [v, setV] = useState(value)
  useEffect(() => setV(value), [value])
  if (ro) return <div className="task-title">{value}</div>
  return <input className="task-title input-bare" maxLength={100} value={v} onChange={e => setV(e.target.value)} onBlur={() => { const t = v.trim(); if (t && t !== value) onSave(t); else setV(value) }} onKeyDown={e => e.key === 'Enter' && (e.target as HTMLInputElement).blur()} />
}

function BodyEdit({ value, ro, onSave }: { value: string; ro: boolean; onSave: (v: string) => void }) {
  const [v, setV] = useState(value)
  useEffect(() => setV(value), [value])
  if (ro) return value ? <div className="task-body">{value}</div> : null
  return <textarea className="textarea" rows={3} maxLength={5000} placeholder="补充说明（可空）" value={v} onChange={e => setV(e.target.value)} onBlur={() => v !== value && onSave(v)} />
}

function Watchers({ list, people, ro, current, onChange }: { list: { id: number; name: string }[]; people: Assignee[]; ro: boolean; current: number[]; onChange: (ids: number[]) => void }) {
  const rest = people.filter(p => !current.includes(p.id))
  return (
    <span className="inline" style={{ flexWrap: 'wrap', gap: 4 }}>
      {list.length === 0 && ro && <span className="t4">无</span>}
      {list.map(w => (
        <span key={w.id} className="tag tag-gray">{w.name}{!ro && <button className="tag-x" aria-label="移除" onClick={() => onChange(current.filter(x => x !== w.id))}><X /></button>}</span>
      ))}
      {!ro && rest.length > 0 && (
        <select className="input" style={{ width: 120, height: 24 }} value="" onChange={e => e.target.value && onChange([...current, Number(e.target.value)])}>
          <option value="">+ 加协作人</option>
          {rest.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
        </select>
      )}
    </span>
  )
}

function Checklist({ items, canTick, canEdit, onChange }: { items: Task['checklist']; canTick: boolean; canEdit: boolean; onChange: (c: Task['checklist']) => void }) {
  const [add, setAdd] = useState('')
  const done = items.filter(i => i.done).length
  if (!items.length && !canEdit) return null
  const push = () => { const t = add.trim(); if (!t) return; onChange([...items, { text: t, done: false }]); setAdd('') }
  return (
    <div className="task-check">
      <div className="row"><CheckSquare size={14} className="t3" /><span className="sub-title" style={{ margin: 0 }}>检查清单</span>{items.length > 0 && <span className="t3 mono fs-12">{done}/{items.length}</span>}
        {items.length > 0 && <span className="task-progress grow"><i style={{ width: `${(done / items.length) * 100}%` }} /></span>}
      </div>
      <div className="subtasks">
        {items.map((c, i) => (
          <label key={i}>
            <input type="checkbox" className="check" checked={c.done} disabled={!canTick} onChange={() => onChange(items.map((x, k) => (k === i ? { ...x, done: !x.done } : x)))} />
            <span className={'grow' + (c.done ? ' done' : '')}>{c.text}</span>
            {canEdit && <button className="tag-x" aria-label="删除这一项" onClick={e => { e.preventDefault(); onChange(items.filter((_, k) => k !== i)) }}><X /></button>}
          </label>
        ))}
      </div>
      {canEdit && items.length < 50 && (
        <div className="row"><input className="input" style={{ height: 28 }} placeholder="加一项，回车保存" value={add} onChange={e => setAdd(e.target.value)} onKeyDown={e => e.key === 'Enter' && push()} />
          <button className="btn btn-ghost btn-sm" onClick={push}><Plus />加</button></div>
      )}
    </div>
  )
}

// 评论里的 @名字 高亮
function highlight(s: string) {
  return s.split(/(@[^\s@，。,.:：;；!！?？]+)/g).map((p, i) => (p.startsWith('@') ? <span key={i} className="mention">{p}</span> : p))
}

// 评论框：打 @ 弹出能派活的人，选了插入「@名字 」
function CommentBox({ taskId, people, meId, onSent }: { taskId: number; people: Assignee[]; meId?: number; onSent: () => void }) {
  const toast = useToast()
  const ta = useRef<HTMLTextAreaElement>(null)
  const [v, setV] = useState('')
  const [q, setQ] = useState<string | null>(null)
  const [hi, setHi] = useState(0)
  const [busy, setBusy] = useState(false)
  const cands = useMemo(() => (q === null ? [] : people.filter(p => p.id !== meId && p.name.toLowerCase().includes(q.toLowerCase())).slice(0, 6)), [q, people, meId])
  const onInput = (val: string) => {
    setV(val)
    const pos = ta.current?.selectionStart ?? val.length
    const m = /@([^\s@]*)$/.exec(val.slice(0, pos))
    setQ(m ? m[1] : null); setHi(0)
  }
  const pick = (p: Assignee) => {
    const pos = ta.current?.selectionStart ?? v.length
    const before = v.slice(0, pos).replace(/@([^\s@]*)$/, `@${p.name} `)
    setV(before + v.slice(pos)); setQ(null)
    requestAnimationFrame(() => { ta.current?.focus(); ta.current?.setSelectionRange(before.length, before.length) })
  }
  const send = async () => {
    const body = v.trim()
    if (!body) return
    setBusy(true)
    try {
      const r = await api<{ ignored_mentions?: (string | number | { name?: string })[] }>(`/api/tasks/${taskId}/comments`, { body: { body } })
      const ig = (r.ignored_mentions || []).map(x => (typeof x === 'object' ? x.name : String(x))).filter(Boolean)
      if (ig.length) toast(`${ig.join('、')} 看不到这条任务，没有通知到`, 'error')
      setV(''); onSent()
    } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  return (
    <div className="comment-box">
      {cands.length > 0 && (
        <div className="menu mention-menu">
          {cands.map((p, i) => (
            <div key={p.id} className={'menu-item' + (i === hi ? ' on' : '')} onMouseDown={e => { e.preventDefault(); pick(p) }}>
              <span className="avatar xs c2">{initials(p.name)}</span>{p.name}<span className="end fs-12">{p.role_name}</span>
            </div>
          ))}
        </div>
      )}
      <textarea ref={ta} className="textarea" rows={2} maxLength={2000} placeholder="写评论，@ 提到同事会通知他；Ctrl+Enter 发送" value={v}
        onChange={e => onInput(e.target.value)}
        onKeyDown={e => {
          if (cands.length) {
            if (e.key === 'ArrowDown') { e.preventDefault(); setHi((hi + 1) % cands.length); return }
            if (e.key === 'ArrowUp') { e.preventDefault(); setHi((hi + cands.length - 1) % cands.length); return }
            if (e.key === 'Enter' || e.key === 'Tab') { e.preventDefault(); pick(cands[hi]); return }
            if (e.key === 'Escape') { e.stopPropagation(); setQ(null); return }
          }
          if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) send()
        }} />
      <div className="row"><span className="grow" /><button className="btn btn-primary btn-sm" disabled={busy || !v.trim()} onClick={send}><Send />发送</button></div>
    </div>
  )
}

