import { useState } from 'react'
import { useAuth } from '../lib/auth'
import { Topbar, PageHead, Tabs } from '../components/ui'
import { UsersTab } from './admin/Users'
import { RolesTab } from './admin/Roles'
import { ShopsTab } from './admin/Shops'
import { UsageTab } from './admin/Usage'
import { AuditTab } from './admin/Audit'

type Key = 'users' | 'roles' | 'shops' | 'usage' | 'audit'

export function Admin() {
  const { can } = useAuth()
  const items = ([
    { key: 'users', label: '账号', perm: 'admin.users' },
    { key: 'roles', label: '角色和权限', perm: 'admin.users' },
    { key: 'shops', label: '店铺', perm: 'admin.shops' },
    { key: 'usage', label: 'AI 用量', perm: 'admin.users' },
    { key: 'audit', label: '操作日志', perm: 'admin.audit' }
  ] as { key: Key; label: string; perm: string }[]).filter(i => can(i.perm))
  const [tab, setTab] = useState<Key>(items[0]?.key || 'users')
  return (
    <>
      <Topbar crumbs={['账号与权限', items.find(i => i.key === tab)?.label || '']} />
      <div className="scroll"><main className="content">
        <PageHead title="账号与权限" sub="管账号、角色、店铺分配、AI 额度和操作记录" />
        <Tabs value={tab} onChange={setTab} items={items} />
        {tab === 'users' && <UsersTab />}
        {tab === 'roles' && <RolesTab />}
        {tab === 'shops' && <ShopsTab />}
        {tab === 'usage' && <UsageTab />}
        {tab === 'audit' && <AuditTab />}
      </main></div>
    </>
  )
}
