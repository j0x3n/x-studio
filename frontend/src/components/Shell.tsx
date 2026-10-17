import { NavLink, Outlet } from 'react-router-dom'
import { Gem, LayoutGrid, Package, Images, Wand2, PenLine, LayoutTemplate, ClipboardCheck, Users, Settings, LogOut } from 'lucide-react'
import { useAuth } from '../lib/auth'
import { initials } from '../lib/format'

// 左侧 64px 图标栏（workspace 预设）。没有权限的入口不显示。
const NAV = [
  { to: '/', label: '工作台', icon: LayoutGrid, perm: '' },
  { to: '/products', label: '商品', icon: Package, perm: 'product.view' },
  { to: '/assets', label: '素材', icon: Images, perm: 'product.view' },
  { to: '/process', label: '处理', icon: Wand2, perm: 'asset.upload' },
  { to: '/copy', label: '文案', icon: PenLine, perm: 'copy.view' },
  { to: '/templates', label: '模板', icon: LayoutTemplate, perm: 'product.view' },
  { to: '/review', label: '审核', icon: ClipboardCheck, perm: 'render.review' }
]

export function Shell() {
  const { me, can, logout } = useAuth()
  const name = me?.user.display_name || me?.user.username || ''
  const admin = can('admin.users') || can('admin.shops') || can('admin.audit')
  return (
    <div className="app">
      <div className="shell">
        <nav className="rail">
          <NavLink to="/" className="rail-logo" title="X Studio"><Gem /></NavLink>
          {NAV.filter(n => !n.perm || can(n.perm)).map(n => (
            <NavLink key={n.to} to={n.to} end={n.to === '/'} className={({ isActive }) => 'rail-item' + (isActive ? ' active' : '')}>
              <n.icon />{n.label}
            </NavLink>
          ))}
          <span className="rail-spacer" />
          {admin && <NavLink to="/admin" className={({ isActive }) => 'rail-item' + (isActive ? ' active' : '')}><Users />账号</NavLink>}
          {can('admin.settings') && <NavLink to="/settings" className={({ isActive }) => 'rail-item' + (isActive ? ' active' : '')}><Settings />设置</NavLink>}
          <button className="rail-item" onClick={logout} title="退出登录"><LogOut />退出</button>
          <span className="avatar lg" title={`${name} · ${me?.role.name ?? ''}`} style={{ marginTop: 6 }}>{initials(name)}</span>
        </nav>
        <div className="main">
          <Outlet />
        </div>
      </div>
    </div>
  )
}
