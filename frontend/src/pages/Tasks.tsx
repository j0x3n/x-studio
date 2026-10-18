import { useCallback, useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { Plus, Search, Table2, Columns3, MessageSquare, CheckSquare, Users } from 'lucide-react'
import { api, qs, type Page } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useNotify } from '../lib/notify'
import type { Task, TaskStatus } from '../lib/types'
import { taskStatus, taskPriority, OPEN } from '../lib/tasks'
import { fmtAgo } from '../lib/format'
import { Topbar, Loading, Empty, Tabs } from '../components/ui'
import { TaskDrawer, NewTaskDrawer, StatusPill, PriorityTag, Due, Person, useAssignees } from '../components/TaskViews'

type View = 'mine' | 'created' | 'watching' | 'all' | 'load'
type Load = { id: number; name: string; role_name?: string; open: number; overdue: number; due_today: number; done_7d: number }
const VIEW_KEY = 'tasks.layout'
const COLS: TaskStatus[] = ['todo', 'doing', 'waiting', 'done']

export function Tasks() {
  const { id } = useParams()
  const nav = useNavigate()
  const { can, isAdmin } = useAuth()
  const { latest } = useNotify()
  const manage = isAdmin || can('task.manage')
  const [view, setView] = useState<View>('mine')
  const [layout, setLayout] = useState<'table' | 'board'>(() => { try { return localStorage.getItem(VIEW_KEY) === 'board' ? 'board' : 'table' } catch { return 'table' } })
  const [status, setStatus] = useState('open')
  const [priority, setPriority] = useState('')
  const [due, setDue] = useState('')
  const [assignee, setAssignee] = useState('')
  const [q, setQ] = useState('')
  const [data, setData] = useState<Page<Task> | null>(null)
  const [counts, setCounts] = useState<Record<string, number>>({})
  const [creating, setCreating] = useState(false)
  const people = useAssignees()

  // 看板按状态分列，所以看板模式下取全部状态（已完成的只看最近的）
  const st = layout === 'board' ? (status === 'open' ? 'todo,doing,waiting,done' : status) : status
  const load = useCallback(() => {
    if (view === 'load') return
    api<Page<Task>>('/api/tasks' + qs({ view, status: st, priority, due, assignee_id: assignee, q, page_size: 200 }))
      .then(setData).catch(() => setData({ items: [], total: 0 }))
  }, [view, st, priority, due, assignee, q])
  useEffect(() => { const t = setTimeout(load, q ? 250 : 0); return () => clearTimeout(t) }, [load, q])
  // 各视图未完成数，标签上显示
  const loadCounts = useCallback(() => {
    const vs = ['mine', 'created', 'watching', 'all'] as const
    Promise.all(vs.map(v => api<Page<Task>>('/api/tasks' + qs({ view: v, status: 'open', page_size: 1 })).then(r => r.total).catch(() => 0)))
      .then(ns => setCounts(Object.fromEntries(vs.map((v, i) => [v, ns[i]]))))
  }, [])
  useEffect(() => { loadCounts() }, [loadCounts])
  // 有新的任务类通知时刷新
  useEffect(() => { if (latest?.type.startsWith('task_')) { load(); loadCounts() } }, [latest]) // eslint-disable-line react-hooks/exhaustive-deps

  const pickLayout = (v: 'table' | 'board') => { setLayout(v); try { localStorage.setItem(VIEW_KEY, v) } catch { /* 无痕模式 */ } }
  const open = (t: { id: number }) => nav(`/tasks/${t.id}`)
  const changed = () => { load(); loadCounts() }
  const items = data?.items || []
  const filtered = !!(priority || due || assignee || q || status !== 'open')
  const overdue = items.filter(t => t.overdue && OPEN.includes(t.status)).length

  return (
    <>
      <Topbar title="任务" sub={<>{counts.mine ?? 0} 件我负责的未完成{overdue > 0 && view !== 'load' && <><span className="sep" /><span className="danger-text">{overdue} 件逾期</span></>}</>}>
        <button className="btn btn-primary" onClick={() => setCreating(true)}><Plus />新建任务</button>
      </Topbar>
      <div className="scroll"><main className="content">
        <Tabs value={view} onChange={v => { setView(v); setData(null) }} items={[
          { key: 'mine', label: '我负责的', count: counts.mine },
          { key: 'created', label: '我建的', count: counts.created },
          { key: 'watching', label: '我协作的', count: counts.watching },
          { key: 'all', label: '全部', count: counts.all },
          ...(manage ? [{ key: 'load' as const, label: '负载' }] : [])
        ]} />
        {view === 'load' ? <LoadView onPick={uid => { setView('all'); setAssignee(String(uid)); setStatus('open') }} /> : (
          <>
            <div className="toolbar">
              <select className="input" style={{ width: 120 }} value={status} onChange={e => setStatus(e.target.value)}>
                <option value="open">未完成</option>
                <option value="">全部状态</option>
                {Object.entries(taskStatus).map(([k, v]) => <option key={k} value={k}>{v.label}</option>)}
              </select>
              <select className="input" style={{ width: 110 }} value={priority} onChange={e => setPriority(e.target.value)}>
                <option value="">全部优先级</option>
                {Object.entries(taskPriority).map(([k, v]) => <option key={k} value={k}>{v.label}</option>)}
              </select>
              <div className="segmented">
                {[['', '不限'], ['overdue', '逾期'], ['today', '今天到期'], ['week', '本周']].map(([k, l]) => <button key={k} className={due === k ? 'active' : ''} onClick={() => setDue(k)}>{l}</button>)}
              </div>
              {view !== 'mine' && people.length > 0 && (
                <select className="input" style={{ width: 130 }} value={assignee} onChange={e => setAssignee(e.target.value)}>
                  <option value="">所有负责人</option>
                  <option value="0">没派出去的</option>
                  {people.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
                </select>
              )}
              <span className="spacer" />
              <label className="field" style={{ width: 200 }}><Search /><input placeholder="搜索标题" value={q} onChange={e => setQ(e.target.value)} /></label>
              <div className="segmented">
                <button className={layout === 'table' ? 'active' : ''} onClick={() => pickLayout('table')}><Table2 />列表</button>
                <button className={layout === 'board' ? 'active' : ''} onClick={() => pickLayout('board')}><Columns3 />看板</button>
              </div>
            </div>
            {!data ? <section className="panel"><Loading /></section> : layout === 'table' ? (
              <section className="panel">
                {items.length === 0 ? <Empty title={filtered ? '没有符合条件的任务' : view === 'mine' ? '手上没有任务' : '没有任务'} desc={filtered ? undefined : '点右上角「新建任务」，或在「设置 → 自动派活」里让系统按事件自动派。'} /> : (
                  <div className="table-wrap"><table className="table">
                    <thead><tr><th>任务</th><th>状态</th><th>优先级</th><th>负责人</th><th>截止</th><th className="num">清单</th><th className="num">评论</th><th>更新</th></tr></thead>
                    <tbody>{items.map(t => (
                      <tr key={t.id} className={'clickable' + (Number(id) === t.id ? ' selected' : '')} onClick={() => open(t)}>
                        <td className="wrap"><div className="cell-title">{t.title}</div><div className="cell-sub"><span className="mono">#{t.id}</span>{t.target && ` · ${t.target.label}`}{t.rule_id ? ' · 自动派活' : ''}</div></td>
                        <td><StatusPill s={t.status} /></td>
                        <td><PriorityTag p={t.priority} /></td>
                        <td><Person p={t.assignee} /></td>
                        <td><Due t={t} /></td>
                        <td className="num mono">{t.checklist.length ? `${t.checklist_done}/${t.checklist.length}` : <span className="t4">—</span>}</td>
                        <td className="num mono">{t.comment_count || <span className="t4">—</span>}</td>
                        <td className="t3">{fmtAgo(t.updated_at)}</td>
                      </tr>
                    ))}</tbody>
                  </table></div>
                )}
                {data.total > items.length && <div className="pager">只显示前 {items.length} 条，共 {data.total} 条，缩小筛选范围看其余的</div>}
              </section>
            ) : (
              <div className="kanban">
                {(status === 'open' || status === '' ? COLS : [status as TaskStatus]).map(s => {
                  const col = items.filter(t => t.status === s)
                  return (
                    <div key={s} className="kcol">
                      <div className="kcol-head"><StatusPill s={s} /><span className="count">{col.length}</span></div>
                      {col.map(t => (
                        <div key={t.id} className={'kcard' + (t.overdue && OPEN.includes(t.status) ? ' late' : '')} onClick={() => open(t)}>
                          {(t.priority === 'high' || t.priority === 'urgent' || t.rule_id) && <div className="tags"><PriorityTag p={t.priority} quiet />{t.rule_id ? <span className="tag tag-gray">自动派活</span> : null}</div>}
                          <div className="title">{t.title}</div>
                          {t.target && <div className="desc ellipsis">{t.target.label}</div>}
                          <div className="foot">
                            <Person p={t.assignee} />
                            <span className="counters">
                              {t.checklist.length > 0 && <span><CheckSquare />{t.checklist_done}/{t.checklist.length}</span>}
                              {t.comment_count > 0 && <span><MessageSquare />{t.comment_count}</span>}
                              {t.due_at && <Due t={t} />}
                            </span>
                          </div>
                        </div>
                      ))}
                      {col.length === 0 && <div className="kempty">没有</div>}
                    </div>
                  )
                })}
              </div>
            )}
          </>
        )}
      </main></div>
      {creating && <NewTaskDrawer onClose={() => setCreating(false)} onCreated={t => { setCreating(false); changed(); open(t) }} />}
      {id && <TaskDrawer key={id} id={Number(id)} onClose={() => nav('/tasks')} onChanged={changed} />}
    </>
  )
}

// 每人手上的活：管理者派活前看一眼
function LoadView({ onPick }: { onPick: (uid: number) => void }) {
  const [rows, setRows] = useState<Load[] | null>(null)
  useEffect(() => {
    api<{ items: Load[] } | Load[]>('/api/tasks/load').then(r => setRows(Array.isArray(r) ? r : r.items || [])).catch(() => setRows([]))
  }, [])
  if (!rows) return <section className="panel"><Loading /></section>
  const max = Math.max(1, ...rows.map(r => r.open))
  return (
    <section className="panel">
      <div className="panel-head"><div className="titles"><div className="panel-title">负载</div><div className="panel-sub">只算你看得到的任务；点一行看这个人手上的任务</div></div><Users className="t3" size={16} /></div>
      {rows.length === 0 ? <Empty title="还没有能派活的人" desc="给角色加上「看任务」权限后，这里会列出来。" /> : (
        <div className="table-wrap"><table className="table">
          <thead><tr><th>人</th><th style={{ width: '34%' }}>未完成</th><th className="num">逾期</th><th className="num">今天到期</th><th className="num">近 7 天完成</th></tr></thead>
          <tbody>{rows.map(r => (
            <tr key={r.id} className="clickable" onClick={() => onPick(r.id)}>
              <td><Person p={{ id: r.id, name: r.name }} />{r.role_name && <span className="t4 fs-12" style={{ marginLeft: 6 }}>{r.role_name}</span>}</td>
              <td><span className="load-bar"><i style={{ width: `${(r.open / max) * 100}%` }} /></span><span className="mono t1">{r.open}</span></td>
              <td className="num mono">{r.overdue ? <span className="danger-text">{r.overdue}</span> : <span className="t4">0</span>}</td>
              <td className="num mono">{r.due_today || <span className="t4">0</span>}</td>
              <td className="num mono">{r.done_7d || <span className="t4">0</span>}</td>
            </tr>
          ))}</tbody>
        </table></div>
      )}
    </section>
  )
}
