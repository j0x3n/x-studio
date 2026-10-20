import { useCallback, useEffect, useState } from 'react'
import { Plus, Pencil, Trash2 } from 'lucide-react'
import { api } from '../lib/api'
import { useToast, errText } from '../lib/toast'
import type { Role, TaskPriority, TaskRule } from '../lib/types'
import { ruleEvent, assignMode, taskPriority } from '../lib/tasks'
import { Drawer, Empty, Field, Loading } from '../components/ui'
import { PriorityTag, useAssignees } from '../components/TaskViews'

const EMPTY: Omit<TaskRule, 'id'> = { name: '', enabled: true, event: 'product.created', title_tpl: '', body_tpl: '', assign_mode: 'role_round_robin', assign_user_id: null, assign_role_id: null, due_hours: null, priority: 'normal' }
const NEEDS_ROLE = ['role_round_robin', 'role_least_loaded']

// 设置 → 自动派活：某个事件发生时自动建任务，派给谁按规则来
export function TaskRulesTab() {
  const toast = useToast()
  const [rules, setRules] = useState<TaskRule[] | null>(null)
  const [roles, setRoles] = useState<Role[]>([])
  const [edit, setEdit] = useState<Partial<TaskRule> | null>(null)
  const people = useAssignees()
  const load = useCallback(() => {
    api<{ items: TaskRule[] } | TaskRule[]>('/api/task-rules').then(r => setRules(Array.isArray(r) ? r : r.items || [])).catch(e => { toast(errText(e), 'error'); setRules([]) })
  }, [toast])
  useEffect(() => { load() }, [load])
  useEffect(() => { api<{ items: Role[] } | Role[]>('/api/roles').then(r => setRoles(Array.isArray(r) ? r : r.items || [])).catch(() => setRoles([])) }, [])

  const who = (r: TaskRule) => {
    if (r.assign_mode === 'fixed') return people.find(p => p.id === r.assign_user_id)?.name || `账号 #${r.assign_user_id}`
    if (NEEDS_ROLE.includes(r.assign_mode)) return `${roles.find(x => x.id === r.assign_role_id)?.name || `角色 #${r.assign_role_id}`} · ${r.assign_mode === 'role_round_robin' ? '轮流' : '最闲的'}`
    return assignMode[r.assign_mode]
  }
  const toggle = async (r: TaskRule) => {
    try { await api(`/api/task-rules/${r.id}`, { method: 'PATCH', body: { enabled: !r.enabled } }); load() } catch (e) { toast(errText(e), 'error') }
  }
  const remove = async (r: TaskRule) => {
    if (!confirm(`删除规则「${r.name}」？已经建出来的任务不受影响。`)) return
    try { await api(`/api/task-rules/${r.id}`, { method: 'DELETE' }); toast('已删除', 'ok'); load() } catch (e) { toast(errText(e), 'error') }
  }

  return (
    <section className="panel">
      <div className="panel-head">
        <div className="titles"><div className="panel-title">自动派活</div><div className="panel-sub">事件发生时自动建任务。同一条规则对同一个对象只保留一条未完成的任务，不会重复派</div></div>
        <button className="btn btn-primary btn-sm" onClick={() => setEdit({ ...EMPTY })}><Plus />加规则</button>
      </div>
      {!rules ? <Loading /> : rules.length === 0 ? <Empty title="还没有规则" desc="点右上角「加规则」。" /> : (
        <div className="table-wrap"><table className="table">
          <thead><tr><th style={{ width: 52 }}>启用</th><th>规则</th><th>什么时候</th><th>派给</th><th>截止</th><th>优先级</th><th style={{ width: 80 }} /></tr></thead>
          <tbody>{rules.map(r => (
            <tr key={r.id}>
              <td><input type="checkbox" className="switch" checked={r.enabled} onChange={() => toggle(r)} aria-label="启用" /></td>
              <td className="wrap"><div className="cell-title">{r.name}</div><div className="cell-sub mono">{r.title_tpl}</div></td>
              <td>{ruleEvent[r.event] || r.event}</td>
              <td>{who(r)}</td>
              <td className="mono">{r.due_hours ? `${r.due_hours} 小时` : <span className="t4">—</span>}</td>
              <td><PriorityTag p={r.priority} /></td>
              <td className="right">
                <button className="btn btn-ghost btn-icon btn-sm" title="编辑" aria-label="编辑" onClick={() => setEdit(r)}><Pencil /></button>
                <button className="btn btn-ghost btn-icon btn-sm" title="删除" aria-label="删除" onClick={() => remove(r)}><Trash2 /></button>
              </td>
            </tr>
          ))}</tbody>
        </table></div>
      )}
      {edit && <RuleDrawer rule={edit} roles={roles} people={people} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); load() }} />}
    </section>
  )
}

function RuleDrawer({ rule, roles, people, onClose, onSaved }: { rule: Partial<TaskRule>; roles: Role[]; people: { id: number; name: string; role_name: string }[]; onClose: () => void; onSaved: () => void }) {
  const toast = useToast()
  const [f, setF] = useState<Partial<TaskRule>>(rule)
  const [busy, setBusy] = useState(false)
  const commerce = f.event === 'inventory.low_stock' || f.event === 'purchase.received'
  const templateHint = f.event === 'inventory.low_stock' ? '可以用 {item} 物料编码和名称、{reason} 触发原因' : f.event === 'purchase.received' ? '可以用 {purchase} 采购单号、{reason} 触发原因' : '可以用 {product} 款名、{shop} 店铺名、{reason} 失败原因或退回意见'
  const set = <K extends keyof TaskRule>(k: K, v: TaskRule[K]) => setF(x => ({ ...x, [k]: v }))
  const save = async () => {
    if (!f.name?.trim()) return toast('起个规则名', 'error')
    if (!f.title_tpl?.trim()) return toast('写一下标题模板', 'error')
    if (f.assign_mode === 'fixed' && !f.assign_user_id) return toast('选派给谁', 'error')
    if (NEEDS_ROLE.includes(f.assign_mode || '') && !f.assign_role_id) return toast('选一个角色', 'error')
    const body = {
      name: f.name.trim(), enabled: f.enabled, event: f.event, title_tpl: f.title_tpl.trim(), body_tpl: f.body_tpl || '',
      assign_mode: f.assign_mode, priority: f.priority, due_hours: f.due_hours || null,
      assign_user_id: f.assign_mode === 'fixed' ? f.assign_user_id : null,
      assign_role_id: NEEDS_ROLE.includes(f.assign_mode || '') ? f.assign_role_id : null
    }
    setBusy(true)
    try {
      if (f.id) await api(`/api/task-rules/${f.id}`, { method: 'PATCH', body })
      else await api('/api/task-rules', { body })
      toast('规则已保存', 'ok'); onSaved()
    } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  return (
    <Drawer title={f.id ? '编辑规则' : '加规则'} width={480} onClose={onClose} foot={<><span className="grow" /><button className="btn btn-secondary" onClick={onClose}>取消</button><button className="btn btn-primary" disabled={busy} onClick={save}>保存</button></>}>
      <Field label="规则名" req><input className="input" value={f.name || ''} onChange={e => set('name', e.target.value)} placeholder="例如：新款自动派拍样" /></Field>
      <Field label="什么时候" req>
        <select className="input" value={f.event} onChange={e => setF(x => ({ ...x, event: e.target.value, ...(['inventory.low_stock', 'purchase.received'].includes(e.target.value) && ['product_owner', 'job_user'].includes(x.assign_mode || '') ? { assign_mode: 'role_least_loaded' } : {}) }))}>
          {Object.entries(ruleEvent).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
        </select>
      </Field>
      {f.event === 'purchase.received' && <p className="help">登记到货时已完成质检入库，这条规则用于后续复核。分批到货会归入同一采购单的未完成任务。</p>}
      {f.event === 'inventory.low_stock' && <p className="help">库存从正常变为低于安全线时触发。持续低库存不会反复派活，恢复后再次跌破会重新触发。</p>}
      <Field label="任务标题" req hint={templateHint}>
        <input className="input mono" maxLength={100} value={f.title_tpl || ''} onChange={e => set('title_tpl', e.target.value)} placeholder="修图：{product}" />
      </Field>
      <Field label="任务说明"><textarea className="textarea mono" rows={3} value={f.body_tpl || ''} onChange={e => set('body_tpl', e.target.value)} placeholder="{reason}" /></Field>
      <Field label="派给">
        <select className="input" value={f.assign_mode} onChange={e => set('assign_mode', e.target.value as TaskRule['assign_mode'])}>
          {Object.entries(assignMode).filter(([k]) => !commerce || !['product_owner', 'job_user'].includes(k)).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
        </select>
      </Field>
      {f.assign_mode === 'fixed' && (
        <Field label="哪个人" req>
          <select className="input" value={f.assign_user_id ?? ''} onChange={e => set('assign_user_id', e.target.value ? Number(e.target.value) : null)}>
            <option value="">选一个人</option>
            {people.map(p => <option key={p.id} value={p.id}>{p.name} · {p.role_name}</option>)}
          </select>
        </Field>
      )}
      {NEEDS_ROLE.includes(f.assign_mode || '') && (
        <Field label="哪个角色" req hint="只派给未停用、有「看任务」且能查看目标对象的人">
          <select className="input" value={f.assign_role_id ?? ''} onChange={e => set('assign_role_id', e.target.value ? Number(e.target.value) : null)}>
            <option value="">选一个角色</option>
            {roles.map(r => <option key={r.id} value={r.id}>{r.name}</option>)}
          </select>
        </Field>
      )}
      {(f.assign_mode === 'product_owner' || f.assign_mode === 'job_user') && <div className="help" style={{ marginTop: -8 }}>{f.assign_mode === 'product_owner' ? '派给建这款商品的人。' : '派给发起那次出图的人，只对「出图审核被退回」有意义。'}派不出去时任务照建、先不派，并通知有派活权限的人。</div>}
      <div className="form-row">
        <Field label="截止" hint="从事件发生起算，留空不设">
          <input className="input mono" inputMode="numeric" value={f.due_hours ?? ''} onChange={e => set('due_hours', e.target.value ? Math.max(1, Math.floor(Number(e.target.value)) || 0) : null)} placeholder="小时" />
        </Field>
        <Field label="优先级">
          <select className="input" value={f.priority} onChange={e => set('priority', e.target.value as TaskPriority)}>
            {Object.entries(taskPriority).map(([k, v]) => <option key={k} value={k}>{v.label}</option>)}
          </select>
        </Field>
      </div>
      <label className="setting"><input type="checkbox" className="switch" checked={!!f.enabled} onChange={e => set('enabled', e.target.checked)} /><span className="title">启用</span></label>
    </Drawer>
  )
}
