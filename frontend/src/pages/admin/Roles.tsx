import { useCallback, useEffect, useMemo, useState } from 'react'
import { Plus, Save } from 'lucide-react'
import { api, type Page } from '../../lib/api'
import { useToast, errText } from '../../lib/toast'
import type { Permission, Role } from '../../lib/types'
import { Loading, Drawer, Field } from '../../components/ui'
import { scopeName } from '../../lib/format'

// 角色 × 权限点的勾选表。管理员角色固定拥有全部权限，不能改。
export function RolesTab() {
  const toast = useToast()
  const [roles, setRoles] = useState<Role[] | null>(null)
  const [perms, setPerms] = useState<Permission[]>([])
  const [draft, setDraft] = useState<Record<number, string[]>>({})
  const [creating, setCreating] = useState(false)
  const load = useCallback(() => {
    api<Page<Role>>('/api/roles?page_size=200').then(r => { setRoles(r.items); setDraft({}) }).catch(() => setRoles([]))
    api<Permission[]>('/api/permissions').then(setPerms).catch(() => {})
  }, [])
  useEffect(load, [load])
  const groups = useMemo(() => {
    const g: [string, Permission[]][] = []
    for (const p of perms) {
      const last = g[g.length - 1]
      if (last && last[0] === p.group) last[1].push(p); else g.push([p.group, [p]])
    }
    return g
  }, [perms])
  if (!roles) return <Loading />
  const permsOf = (r: Role) => draft[r.id] ?? r.perms
  const toggle = (r: Role, key: string) => {
    const cur = permsOf(r)
    setDraft(d => ({ ...d, [r.id]: cur.includes(key) ? cur.filter(k => k !== key) : [...cur, key] }))
  }
  const dirty = Object.keys(draft).length
  const save = async () => {
    try {
      for (const [id, p] of Object.entries(draft)) await api(`/api/roles/${id}`, { method: 'PATCH', body: { perms: p } })
      toast(`已保存 ${dirty} 个角色。相关账号需要重新登录。`, 'ok'); load()
    } catch (e) { toast(errText(e), 'error') }
  }
  const remove = async (r: Role) => {
    if (!confirm(`删除角色「${r.name}」？`)) return
    try { await api(`/api/roles/${r.id}`, { method: 'DELETE' }); load() } catch (e) { toast(errText(e), 'error') }
  }
  return (
    <section className="panel">
      <div className="panel-head">
        <div className="titles"><div className="panel-title">角色和权限</div><div className="panel-sub">勾选每个角色能做什么。改完点保存，相关账号会被要求重新登录。</div></div>
        <span className="grow" />
        <button className="btn btn-secondary btn-sm" onClick={() => setCreating(true)}><Plus />新建角色</button>
        <button className="btn btn-primary btn-sm" disabled={!dirty} onClick={save}><Save />{dirty ? `保存 ${dirty} 个角色` : '保存'}</button>
      </div>
      <div className="table-wrap"><table className="table matrix">
        <thead><tr><th style={{ minWidth: 200 }}>权限</th>{roles.map(r => (
          <th key={r.id}><div>{r.name}</div><div className="t4 fs-12" style={{ fontWeight: 400 }}>{scopeName[r.scope]}</div>
            {!r.builtin && <button className="btn btn-ghost btn-sm" onClick={() => remove(r)}>删除</button>}</th>
        ))}</tr></thead>
        <tbody>{groups.map(([g, list]) => (
          <GroupRows key={g} group={g} list={list} roles={roles} permsOf={permsOf} toggle={toggle} />
        ))}</tbody>
      </table></div>
      {creating && <NewRole perms={perms} onClose={() => setCreating(false)} onDone={() => { setCreating(false); load() }} />}
    </section>
  )
}

function GroupRows({ group, list, roles, permsOf, toggle }: { group: string; list: Permission[]; roles: Role[]; permsOf: (r: Role) => string[]; toggle: (r: Role, k: string) => void }) {
  return (
    <>
      <tr className="grp"><td colSpan={roles.length + 1}>{group}</td></tr>
      {list.map(p => (
        <tr key={p.key}>
          <td><div>{p.name}</div><div className="cell-sub mono">{p.key}</div></td>
          {roles.map(r => (
            <td key={r.id}>{r.key === 'admin'
              ? <input type="checkbox" className="check" checked disabled />
              : <input type="checkbox" className="check" checked={permsOf(r).includes(p.key)} onChange={() => toggle(r, p.key)} />}</td>
          ))}
        </tr>
      ))}
    </>
  )
}

function NewRole({ perms, onClose, onDone }: { perms: Permission[]; onClose: () => void; onDone: () => void }) {
  const toast = useToast()
  const [name, setName] = useState('')
  const [key, setKey] = useState('')
  const [scope, setScope] = useState<'shops' | 'shares'>('shops')
  const [busy, setBusy] = useState(false)
  const save = async () => {
    setBusy(true)
    try { await api('/api/roles', { body: { key: key.trim(), name: name.trim(), scope, perms: perms.filter(p => p.key === 'product.view').map(p => p.key) } }); toast('已新建，在表里勾选它的权限', 'ok'); onDone() }
    catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  return (
    <Drawer title="新建角色" width={380} onClose={onClose} foot={<>
      <button className="btn btn-secondary" onClick={onClose}>取消</button>
      <button className="btn btn-primary" disabled={busy || !name.trim() || !/^[a-z][a-z0-9_]{1,30}$/.test(key)} onClick={save}>创建</button>
    </>}>
      <Field label="名字" req><input className="input" value={name} onChange={e => setName(e.target.value)} placeholder="如：客服" /></Field>
      <Field label="标识" req hint="小写英文、数字、下划线，如 service"><input className="input mono" value={key} onChange={e => setKey(e.target.value)} /></Field>
      <Field label="数据范围">
        <select className="input" value={scope} onChange={e => setScope(e.target.value as 'shops' | 'shares')}>
          <option value="shops">按分到的店铺</option><option value="shares">只看分享给他的款（外部）</option>
        </select>
      </Field>
    </Drawer>
  )
}
