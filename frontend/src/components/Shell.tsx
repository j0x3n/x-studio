import { useEffect, useState } from 'react'
import { NavLink, Outlet, useLocation } from 'react-router-dom'
import { Gem, Sun, ListTodo, Package, Images, Wand2, PenLine, LayoutTemplate, ClipboardCheck, Users, Settings, LogOut, Camera, type LucideIcon } from 'lucide-react'
import { api } from '../lib/api'
import { useAuth } from '../lib/auth'
import { AssistantProvider, useAssistant } from '../lib/assistant'
import { NotifyProvider, useNotify } from '../lib/notify'
import { initials } from '../lib/format'
import type { Home } from '../lib/types'
import { AssistantPanel } from './AssistantPanel'

// 左侧 232px 侧边栏（clean 预设），按模块分组；窄屏自动收成 64px 图标栏。没有权限的入口不显示。
type Item = { to: string; label: string; icon: LucideIcon; perm?: string; badge?: 'todo' | 'review' | 'tasks' }
const GROUPS: { label?: string; items: Item[] }[] = [
  { items: [
    { to: '/', label: '今天', icon: Sun, badge: 'todo' },
    { to: '/tasks', label: '任务', icon: ListTodo, perm: 'task.view', badge: 'tasks' }
  ] },
  { label: '商品', items: [
    { to: '/products', label: '商品库', icon: Package, perm: 'product.view' },
    { to: '/assets', label: '素材库', icon: Images, perm: 'product.view' },
    { to: '/guide', label: '拍摄要求', icon: Camera }
  ] },
  { label: '生产', items: [
    { to: '/process', label: '图片处理', icon: Wand2, perm: 'asset.upload' },
    { to: '/copy', label: '文案', icon: PenLine, perm: 'copy.view' },
    { to: '/templates', label: '模板', icon: LayoutTemplate, perm: 'product.view' },
    { to: '/review', label: '出图审核', icon: ClipboardCheck, perm: 'render.review', badge: 'review' }
  ] }
]

type Usage = { items: { kind: string; units: number }[] }

export function Shell() {
  return <NotifyProvider><AssistantProvider><ShellInner /></AssistantProvider></NotifyProvider>
}

function ShellInner() {
  const { me, can, logout } = useAuth()
  const { open } = useAssistant()
  const { latest } = useNotify()
  const loc = useLocation()
  const [home, setHome] = useState<Home | null>(null)
  const [usage, setUsage] = useState<Usage | null>(null)
  const name = me?.user.display_name || me?.user.username || ''
  const admin = can('admin.users') || can('admin.shops') || can('admin.audit')

  // 角标和用量：切换页面时顺手刷新，最多一分钟一次
  const [stamp, setStamp] = useState(0)
  useEffect(() => {
    const now = Date.now()
    if (now - stamp < 60_000) return
    setStamp(now)
    api<Home>('/api/home').then(setHome).catch(() => setHome(null))
    api<Usage>('/api/usage/me').then(setUsage).catch(() => setUsage(null))
  }, [loc.pathname]) // eslint-disable-line react-hooks/exhaustive-deps

  // 「任务」角标：我负责的未完成数。切页面或收到任务通知时刷新
  const [openTasks, setOpenTasks] = useState(0)
  const canTasks = can('task.view')
  useEffect(() => {
    if (!canTasks) return
    api<{ total: number }>('/api/tasks?view=mine&status=open&page_size=1').then(r => setOpenTasks(r.total || 0)).catch(() => setOpenTasks(0))
  }, [loc.pathname, canTasks, latest?.id])

  const units = (k: string) => (usage?.items || []).filter(i => i.kind === k).reduce((a, b) => a + (b.units || 0), 0)
  const badge = (b?: Item['badge']) => {
    if (b === 'tasks') return openTasks
    if (!home || !b) return 0
    return b === 'review' ? home.counts.review_pending ?? 0 : home.todos.filter(t => t.level === 'danger').length
  }
  const link = (it: Item) => {
    const n = badge(it.badge)
    return (
      <NavLink key={it.to} to={it.to} end={it.to === '/'} title={it.label} className={({ isActive }) => 'nav-item' + (isActive ? ' active' : '')}>
        <it.icon /><span className="hide-rail">{it.label}</span>
        {n > 0 && <span className={'end badge hide-rail' + (it.badge === 'tasks' ? ' neutral' : '')}>{n}</span>}
      </NavLink>
    )
  }

  return (
    <div className="app">
      <div className="shell">
        <aside className="sidebar">
          <NavLink to="/" className="brand"><span className="brand-mark"><Gem /></span><span className="hide-rail">X Studio</span></NavLink>
          {GROUPS.map((g, i) => {
            const items = g.items.filter(it => !it.perm || can(it.perm))
            if (!items.length) return null
            return (
              <nav key={i} className="nav-group">
                {g.label && <div className="nav-label hide-rail">{g.label}</div>}
                {items.map(link)}
              </nav>
            )
          })}
          {(admin || can('admin.settings')) && (
            <nav className="nav-group">
              <div className="nav-label hide-rail">管理</div>
              {admin && link({ to: '/admin', label: '账号与权限', icon: Users })}
              {can('admin.settings') && link({ to: '/settings', label: '设置', icon: Settings })}
            </nav>
          )}
          <div className="sidebar-foot">
            {usage && (
              <div className="side-usage hide-rail">
                <span className="t3">本月 AI</span>
                <span className="mono t1">{units('image')}</span><span className="t3">张图</span>
                <span className="mono t1">{units('text')}</span><span className="t3">次文本</span>
              </div>
            )}
            <div className="user-row">
              <span className="avatar lg c1" title={name}>{initials(name)}</span>
              <div className="grow hide-rail" style={{ minWidth: 0 }}>
                <div className="name ellipsis">{name}</div>
                <div className="role ellipsis">{me?.role.name}</div>
              </div>
              <button className="btn btn-ghost btn-icon btn-sm" onClick={logout} title="退出登录" aria-label="退出登录"><LogOut /></button>
            </div>
          </div>
        </aside>
        <div className="main">
          <Outlet />
        </div>
        {open && <AssistantPanel />}
      </div>
    </div>
  )
}
