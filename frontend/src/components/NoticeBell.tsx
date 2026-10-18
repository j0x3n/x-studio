import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Bell, CheckCheck, Settings2, BellRing, ListTodo, AtSign, MessageSquare, Clock, AlertTriangle, CheckCircle2, ClipboardCheck, Gauge, UserX, type LucideIcon } from 'lucide-react'
import { api, qs, type Page } from '../lib/api'
import { useNotify } from '../lib/notify'
import { useToast, errText } from '../lib/toast'
import { noticeType } from '../lib/tasks'
import { fmtAgo } from '../lib/format'
import type { Notice, NoticePrefs } from '../lib/types'
import { Drawer, Empty, Loading } from './ui'

const LOOK: Record<string, { icon: LucideIcon; tint: string }> = {
  task_assigned: { icon: ListTodo, tint: 'tint-blue' },
  task_mentioned: { icon: AtSign, tint: 'tint-purple' },
  task_comment: { icon: MessageSquare, tint: 'tint-blue' },
  task_due: { icon: Clock, tint: 'tint-orange' },
  task_overdue: { icon: AlertTriangle, tint: 'tint-red' },
  task_unassigned: { icon: UserX, tint: 'tint-orange' },
  task_done: { icon: CheckCircle2, tint: 'tint-green' },
  job_failed: { icon: AlertTriangle, tint: 'tint-red' },
  job_done: { icon: CheckCircle2, tint: 'tint-green' },
  review_result: { icon: ClipboardCheck, tint: 'tint-purple' },
  quota_80: { icon: Gauge, tint: 'tint-orange' }
}

// 顶栏的铃铛：未读数角标，点开是最近的通知
export function NoticeBell() {
  const { unread, latest, markRead } = useNotify()
  const [open, setOpen] = useState(false)
  const [prefs, setPrefs] = useState(false)
  const box = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const f = (e: MouseEvent) => { if (box.current && !box.current.contains(e.target as Node)) setOpen(false) }
    const k = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false)
    document.addEventListener('mousedown', f); addEventListener('keydown', k)
    return () => { document.removeEventListener('mousedown', f); removeEventListener('keydown', k) }
  }, [open])
  return (
    <div className="bell-wrap" ref={box}>
      <button className={'btn btn-ghost btn-icon bell' + (open ? ' on' : '')} onClick={() => setOpen(!open)} title="通知" aria-label={`通知，${unread} 条未读`}>
        <Bell />{unread > 0 && <span className="badge">{unread > 99 ? '99+' : unread}</span>}
      </button>
      {open && <NoticePop latestId={latest?.id} unread={unread} markRead={markRead} onClose={() => setOpen(false)} onPrefs={() => { setOpen(false); setPrefs(true) }} />}
      {prefs && <PrefsDrawer onClose={() => setPrefs(false)} />}
    </div>
  )
}

function NoticePop({ latestId, unread, markRead, onClose, onPrefs }: { latestId?: number; unread: number; markRead: (ids: number[] | 'all') => Promise<void>; onClose: () => void; onPrefs: () => void }) {
  const nav = useNavigate()
  const toast = useToast()
  const { desktop, enableDesktop } = useNotify()
  const [only, setOnly] = useState(false)
  const [list, setList] = useState<Notice[] | null>(null)
  const load = useCallback(() => {
    api<Page<Notice>>('/api/notifications' + qs({ unread: only ? 1 : undefined, page_size: 30 })).then(r => setList(r.items || [])).catch(() => setList([]))
  }, [only])
  useEffect(() => { load() }, [load, latestId])
  const go = async (n: Notice) => {
    if (!n.read_at) { try { await markRead([n.id]) } catch { /* 标不上不影响跳转 */ } }
    onClose()
    if (n.link) nav(n.link)
  }
  const all = async () => {
    try { await markRead('all'); load() } catch (e) { toast(errText(e), 'error') }
  }
  return (
    <div className="bell-pop">
      <div className="bell-head">
        <b className="t1">通知</b>
        <div className="segmented"><button className={only ? '' : 'active'} onClick={() => setOnly(false)}>全部</button><button className={only ? 'active' : ''} onClick={() => setOnly(true)}>未读{unread > 0 && ` ${unread}`}</button></div>
        <span className="grow" />
        <button className="btn btn-ghost btn-sm" disabled={!unread} onClick={all}><CheckCheck />全部已读</button>
        <button className="btn btn-ghost btn-icon btn-sm" title="通知设置" aria-label="通知设置" onClick={onPrefs}><Settings2 /></button>
      </div>
      {desktop === 'default' && (
        <div className="bell-tip"><BellRing size={14} /><span className="grow">开启后，网页开着时有新通知会弹系统提醒</span><button className="btn btn-soft btn-sm" onClick={enableDesktop}>开启桌面通知</button></div>
      )}
      {desktop === 'denied' && <div className="bell-tip t3">浏览器已拦截桌面通知，要在地址栏左边的网站设置里允许</div>}
      <div className="bell-list">
        {!list ? <Loading /> : list.length === 0 ? <Empty title={only ? '没有未读通知' : '还没有通知'} desc="任务派给你、被 @、出图失败或审核有结果时，会在这里提醒。" /> : list.map(n => {
          const look = LOOK[n.type] || LOOK.task_assigned
          return (
            <button key={n.id} className={'bell-item' + (n.read_at ? '' : ' unread')} onClick={() => go(n)}>
              <span className={'icon-tile sm ' + look.tint}><look.icon /></span>
              <span className="grow" style={{ minWidth: 0 }}>
                <span className="title">{n.title}</span>
                {n.body && <span className="body">{n.body}</span>}
                <span className="meta">{noticeType[n.type] || n.type} · {fmtAgo(n.created_at)}</span>
              </span>
              {!n.read_at && <span className="unread-dot" />}
            </button>
          )
        })}
      </div>
    </div>
  )
}

// 通知设置：每种通知收不收、弹不弹桌面提醒，以及免打扰时段
function PrefsDrawer({ onClose }: { onClose: () => void }) {
  const toast = useToast()
  const { desktop, enableDesktop } = useNotify()
  const [p, setP] = useState<NoticePrefs | null>(null)
  const [busy, setBusy] = useState(false)
  const closeRef = useRef(onClose)
  closeRef.current = onClose
  useEffect(() => {
    api<NoticePrefs | { prefs: NoticePrefs }>('/api/notification-prefs')
      .then(r => { const x = 'prefs' in r ? r.prefs : r; setP({ types: x?.types || {}, quiet: x?.quiet ?? null }) })
      .catch(e => { toast(errText(e), 'error'); closeRef.current() })
  }, [toast])
  if (!p) return <Drawer title="通知设置" onClose={onClose}><Loading /></Drawer>
  const get = (t: string) => ({ inbox: p.types[t]?.inbox ?? true, desktop: p.types[t]?.desktop ?? true })
  const set = (t: string, k: 'inbox' | 'desktop', v: boolean) => setP({ ...p, types: { ...p.types, [t]: { ...get(t), [k]: v } } })
  const save = async () => {
    setBusy(true)
    try { await api('/api/notification-prefs', { method: 'PUT', body: p }); toast('通知设置已保存', 'ok'); onClose() }
    catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  return (
    <Drawer title="通知设置" width={460} onClose={onClose} foot={<><span className="grow" /><button className="btn btn-secondary" onClick={onClose}>取消</button><button className="btn btn-primary" disabled={busy} onClick={save}>保存</button></>}>
      {desktop !== 'granted' && desktop !== 'unsupported' && (
        <div className="bell-tip boxed"><BellRing size={14} /><span className="grow">{desktop === 'denied' ? '浏览器拦截了桌面通知，下面的「桌面」开关暂时不起作用' : '这个浏览器还没允许桌面通知'}</span>{desktop === 'default' && <button className="btn btn-soft btn-sm" onClick={enableDesktop}>允许</button>}</div>
      )}
      <table className="table">
        <thead><tr><th>类型</th><th className="num" style={{ width: 64 }}>站内</th><th className="num" style={{ width: 64 }}>桌面</th></tr></thead>
        <tbody>{Object.entries(noticeType).map(([t, label]) => {
          const v = get(t)
          return (
            <tr key={t}>
              <td>{label}</td>
              <td className="num"><input type="checkbox" className="switch" checked={v.inbox} onChange={e => set(t, 'inbox', e.target.checked)} aria-label={`${label} 站内`} /></td>
              <td className="num"><input type="checkbox" className="switch" checked={v.inbox && v.desktop} disabled={!v.inbox} onChange={e => set(t, 'desktop', e.target.checked)} aria-label={`${label} 桌面`} /></td>
            </tr>
          )
        })}</tbody>
      </table>
      <div className="help" style={{ marginTop: -6 }}>关掉「站内」后这类通知不再记录，铃铛里也看不到。</div>
      <div className="setting">
        <div className="grow"><div className="title">免打扰时段</div><div className="desc">这段时间只记在铃铛里，不弹桌面提醒</div></div>
        <input type="checkbox" className="switch" checked={!!p.quiet} onChange={e => setP({ ...p, quiet: e.target.checked ? { from: '22:00', to: '08:00' } : null })} aria-label="免打扰时段" />
      </div>
      {p.quiet && (
        <div className="row">
          <input className="input mono" type="time" style={{ width: 120 }} value={p.quiet.from} onChange={e => setP({ ...p, quiet: { ...p.quiet!, from: e.target.value } })} />
          <span className="t3">到</span>
          <input className="input mono" type="time" style={{ width: 120 }} value={p.quiet.to} onChange={e => setP({ ...p, quiet: { ...p.quiet!, to: e.target.value } })} />
        </div>
      )}
    </Drawer>
  )
}
