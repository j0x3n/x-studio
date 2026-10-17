import { useCallback, useEffect, useState } from 'react'
import { Plus } from 'lucide-react'
import { api, type Page } from '../../lib/api'
import { useAuth } from '../../lib/auth'
import { useToast, errText } from '../../lib/toast'
import type { Role, Shop, UserRow } from '../../lib/types'
import { Pill, Loading, Drawer, Field } from '../../components/ui'
import { fmtDate, initials, scopeName } from '../../lib/format'

export function UsersTab() {
  const { me } = useAuth()
  const [users, setUsers] = useState<UserRow[] | null>(null)
  const [roles, setRoles] = useState<Role[]>([])
  const [shops, setShops] = useState<Shop[]>([])
  const [edit, setEdit] = useState<UserRow | 'new' | null>(null)
  const load = useCallback(() => {
    api<Page<UserRow>>('/api/users?page_size=200').then(r => setUsers(r.items)).catch(() => setUsers([]))
    api<Page<Role> | Role[]>('/api/roles?page_size=200').then(r => setRoles(Array.isArray(r) ? r : r.items)).catch(() => {})
    api<Page<Shop> | Shop[]>('/api/shops?page_size=200').then(r => setShops(Array.isArray(r) ? r : r.items)).catch(() => {})
  }, [])
  useEffect(load, [load])
  if (!users) return <Loading />
  const roleOf = (id: number) => roles.find(r => r.id === id)
  const quotaText = (u: UserRow) => {
    const q = u.quota
    if (!q || (q.month_images == null && q.month_text == null)) return '不限'
    return `图 ${q.month_images ?? '不限'} · 文本 ${q.month_text ?? '不限'}`
  }
  return (
    <section className="panel">
      <div className="panel-head">
        <div className="titles"><div className="panel-title">账号</div><div className="panel-sub">{users.filter(u => !u.disabled).length} 个启用 · {users.filter(u => u.disabled).length} 个停用</div></div>
        <span className="grow" />
        <button className="btn btn-primary btn-sm" onClick={() => setEdit('new')}><Plus />新建账号</button>
      </div>
      <div className="table-wrap"><table className="table">
        <thead><tr><th>账号</th><th>角色</th><th>分到的店铺</th><th>本月 AI 额度</th><th>状态</th><th>创建</th></tr></thead>
        <tbody>{users.map(u => {
          const r = roleOf(u.role_id)
          return (
            <tr key={u.id} className="clickable" onClick={() => setEdit(u)}>
              <td><div className="cell-person"><span className="avatar sm">{initials(u.display_name || u.username)}</span>
                <div><div className="cell-title">{u.display_name || u.username}{u.id === me?.user.id && <span className="t4"> （我）</span>}</div><div className="cell-sub mono">{u.username}</div></div></div></td>
              <td>{r?.name || '—'}{r && r.scope === 'shares' && <span className="t4 fs-12"> · 外部</span>}</td>
              <td>{r?.scope === 'all' ? <span className="t3">全部</span> : r?.scope === 'shares' ? <span className="t3">只看分享的款</span> :
                (u.shop_ids || []).map(id => shops.find(s => s.id === id)?.name).filter(Boolean).join('、') || <span className="t4">未分配</span>}</td>
              <td className="mono fs-12">{quotaText(u)}</td>
              <td>{u.disabled ? <Pill kind="pill-neutral">停用</Pill> : <Pill kind="pill-success">启用</Pill>}</td>
              <td className="t3">{fmtDate(u.created_at)}</td>
            </tr>
          )
        })}</tbody>
      </table></div>
      {edit && <UserDrawer user={edit === 'new' ? null : edit} roles={roles} shops={shops} onClose={() => setEdit(null)} onDone={() => { setEdit(null); load() }} />}
    </section>
  )
}

function UserDrawer({ user, roles, shops, onClose, onDone }: { user: UserRow | null; roles: Role[]; shops: Shop[]; onClose: () => void; onDone: () => void }) {
  const toast = useToast()
  const [username, setUsername] = useState(user?.username || '')
  const [displayName, setDisplayName] = useState(user?.display_name || '')
  const [password, setPassword] = useState('')
  const [roleId, setRoleId] = useState<number>(user?.role_id || roles.find(r => r.key === 'operator')?.id || roles[0]?.id || 0)
  const [shopIds, setShopIds] = useState<number[]>(user?.shop_ids || [])
  const [disabled, setDisabled] = useState(!!user?.disabled)
  const [qImg, setQImg] = useState(user?.quota?.month_images?.toString() ?? '')
  const [qText, setQText] = useState(user?.quota?.month_text?.toString() ?? '')
  const [busy, setBusy] = useState(false)
  const role = roles.find(r => r.id === roleId)
  const num = (s: string) => (s.trim() === '' ? null : Math.max(0, Math.floor(Number(s))))
  const save = async () => {
    setBusy(true)
    try {
      let id = user?.id
      const body: Record<string, unknown> = { display_name: displayName.trim(), role_id: roleId, shop_ids: role?.scope === 'shops' ? shopIds : [], disabled }
      if (password) body.password = password
      if (!user) {
        const r = await api<{ user: { id: number } }>('/api/users', { body: { ...body, username: username.trim(), password } })
        id = r.user.id
      } else {
        await api(`/api/users/${user.id}`, { method: 'PATCH', body })
      }
      await api(`/api/users/${id}/quota`, { method: 'PUT', body: { month_images: num(qImg), month_text: num(qText) } })
      toast('已保存', 'ok'); onDone()
    } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  return (
    <Drawer title={user ? `编辑账号 · ${user.username}` : '新建账号'} onClose={onClose} foot={<>
      <button className="btn btn-secondary" onClick={onClose}>取消</button>
      <button className="btn btn-primary" disabled={busy || (!user && (!username.trim() || password.length < 8))} onClick={save}>保存</button>
    </>}>
      <div className="form-row">
        <Field label="登录名" req><input className="input mono" value={username} disabled={!!user} onChange={e => setUsername(e.target.value)} /></Field>
        <Field label="显示名"><input className="input" value={displayName} onChange={e => setDisplayName(e.target.value)} placeholder="小王" /></Field>
      </div>
      <Field label={user ? '重设密码' : '初始密码'} req={!user} hint={user ? '不改就留空。改密码后对方要重新登录。' : '8–72 位，发给对方后让他自己改。'}>
        <input className="input" type="password" autoComplete="new-password" value={password} onChange={e => setPassword(e.target.value)} />
      </Field>
      <Field label="角色" hint={role ? `数据范围：${scopeName[role.scope]}` : undefined}>
        <select className="input" value={roleId} onChange={e => setRoleId(Number(e.target.value))}>
          {roles.map(r => <option key={r.id} value={r.id}>{r.name}</option>)}
        </select>
      </Field>
      {role?.scope === 'shops' && (
        <Field label="分到的店铺" hint="只能看到和处理这些店铺的商品和上架版本。">
          <div className="checks">{shops.filter(s => !s.archived).map(s => (
            <label key={s.id}><input type="checkbox" className="check" checked={shopIds.includes(s.id)}
              onChange={e => setShopIds(v => e.target.checked ? [...v, s.id] : v.filter(x => x !== s.id))} />{s.name}</label>
          ))}</div>
        </Field>
      )}
      {role?.scope === 'shares' && <div className="t3 fs-12">外部协作者只能看到别人分享给他的款，在商品页里分享。</div>}
      <div className="form-row">
        <Field label="每月 AI 生图（张）" hint="留空表示不限"><input className="input mono" inputMode="numeric" value={qImg} onChange={e => setQImg(e.target.value)} /></Field>
        <Field label="每月文本 AI（次）" hint="留空表示不限"><input className="input mono" inputMode="numeric" value={qText} onChange={e => setQText(e.target.value)} /></Field>
      </div>
      {user && (
        <label className="setting" style={{ cursor: 'pointer' }}>
          <div><div className="cell-title">停用这个账号</div><div className="fs-12 t3">停用后立即退出登录，数据保留。</div></div>
          <input type="checkbox" className="switch" checked={disabled} onChange={e => setDisabled(e.target.checked)} />
        </label>
      )}
    </Drawer>
  )
}
