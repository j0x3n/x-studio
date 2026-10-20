import { useState } from 'react'
import '../styles/finance.css'
import { useAuth } from '../lib/auth'
import { Topbar, Tabs, Empty } from '../components/ui'
import { FinanceAccounts } from '../components/FinanceAccounts'
import { FinancePayables } from '../components/FinancePayables'
import { FinanceSupplierStatement } from '../components/FinanceSupplierStatement'
import { FinanceExpenses } from '../components/FinanceExpenses'
import { FinanceReconcile } from '../components/FinanceReconcile'
import { FinanceRates } from '../components/FinanceRates'
import { FinanceReports } from '../components/FinanceReports'

export type FinanceTab = 'accounts' | 'payables' | 'statement' | 'expenses' | 'reconcile' | 'rates' | 'reports'

export function Finance() {
  const { me, can, isAdmin } = useAuth()
  const [activeTab, setActiveTab] = useState<FinanceTab>('accounts')

  // 权限与角色范围检查：外部协作者禁止使用财务
  const isExternalShare = !isAdmin && me?.role.scope === 'shares'
  const canView = can('finance.view')

  if (isExternalShare) {
    return (
      <>
        <Topbar title="财务管理" />
        <div className="scroll">
          <main className="content">
            <Empty title="访问受限" desc="外部协作者不能使用财务管理模块" />
          </main>
        </div>
      </>
    )
  }

  if (!canView) {
    return (
      <>
        <Topbar title="财务管理" />
        <div className="scroll">
          <main className="content">
            <Empty title="暂无财务查看权限" desc="请联系管理员开通财务查看权限" />
          </main>
        </div>
      </>
    )
  }

  const tabs: { key: FinanceTab; label: string }[] = [
    { key: 'accounts', label: '账户与流水' },
    { key: 'payables', label: '采购应付' },
    { key: 'statement', label: '供应商对账' },
    { key: 'expenses', label: '费用记账' },
    { key: 'reconcile', label: '账单对账' },
    { key: 'rates', label: '月度汇率' },
    { key: 'reports', label: '利润报表' }
  ]

  return (
    <>
      <Topbar
        crumbs={['财务管理']}
        title="财务管理"
        sub="资金账户、采购应付结款、经营费用及通用账单核对"
      />
      <div className="scroll">
        <main className="content" style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
          <Tabs
            value={activeTab}
            onChange={t => setActiveTab(t as FinanceTab)}
            items={tabs}
          />

          <div style={{ width: '100%' }}>
            {activeTab === 'accounts' && <FinanceAccounts />}
            {activeTab === 'payables' && <FinancePayables />}
            {activeTab === 'statement' && <FinanceSupplierStatement />}
            {activeTab === 'expenses' && <FinanceExpenses />}
            {activeTab === 'reconcile' && <FinanceReconcile />}
            {activeTab === 'rates' && <FinanceRates />}
            {activeTab === 'reports' && <FinanceReports />}
          </div>
        </main>
      </div>
    </>
  )
}

export default Finance
