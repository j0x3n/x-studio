import { financeNowInput, financeInputUTC, financeTime } from '../lib/finance'
import { useState } from 'react'
import { Plus, Edit2, Search } from 'lucide-react'
import { api, qs } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { Drawer, Field, Pill, Empty, Loading } from './ui'
import { Pager, useRecord, RecordState } from './CommerceShared'
import {
  type FinanceAccount,
  type FinanceEntry,
  type AccountKind,
  ACCOUNT_KIND_LABELS,
  formatFen,
  yuanToFen,
  fenToYuan
} from '../lib/finance'

export function FinanceAccounts() {
  const { me, can, isAdmin } = useAuth()
  const toast = useToast()
  const canEdit = can('finance.edit')
  const isScopeAll = isAdmin || me?.role.scope === 'all'

  // 账户列表状态
  const [archivedFilter, setArchivedFilter] = useState<'0' | '1' | 'all'>('0')
  const [shopFilter, setShopFilter] = useState<string>('')
  const [accountPage, setAccountPage] = useState(1)

  const accountsQuery = qs({
    archived: archivedFilter,
    shop_id: shopFilter ? Number(shopFilter) : undefined,
    page: accountPage,
    page_size: 50
  })

  const { data: accountsData, error: accountsErr, reload: reloadAccounts } = useRecord<{ items: FinanceAccount[]; total: number }>(
    `/api/finance/accounts${accountsQuery}`
  )

  // 流水明细状态
  const [selectedAccountId, setSelectedAccountId] = useState<number | null>(null)
  const [entryPage, setEntryPage] = useState(1)

  const entriesQuery = qs({
    account_id: selectedAccountId || undefined,
    page: entryPage,
    page_size: 50
  })

  const { data: entriesData, error: entriesErr, reload: reloadEntries } = useRecord<{ items: FinanceEntry[]; total: number }>(
    `/api/finance/entries${entriesQuery}`
  )

  // 账户弹窗抽屉
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [editingAccount, setEditingAccount] = useState<FinanceAccount | null>(null)
  const [accName, setAccName] = useState('')
  const [accKind, setAccKind] = useState<AccountKind>('bank')
  const [accCurrency, setAccCurrency] = useState('CNY')
  const [accShopId, setAccShopId] = useState<string>('')
  const [accOpeningYuan, setAccOpeningYuan] = useState('0.00')
  const [accArchived, setAccArchived] = useState(false)
  const [accSubmitting, setAccSubmitting] = useState(false)

  // 手动记账抽屉
  const [entryDrawerOpen, setEntryDrawerOpen] = useState(false)
  const [entryAccId, setEntryAccId] = useState<number | null>(null)
  const [entryDirection, setEntryDirection] = useState<'in' | 'out'>('out')
  const [entryYuan, setEntryYuan] = useState('')
  const [entryCategory, setEntryCategory] = useState('')
  const [entryNote, setEntryNote] = useState('')
  const [entryOccurred, setEntryOccurred] = useState(() => financeNowInput())
  const [entrySubmitting, setEntrySubmitting] = useState(false)

  const openCreateAccount = () => {
    setEditingAccount(null)
    setAccName('')
    setAccKind('bank')
    setAccCurrency('CNY')
    setAccShopId('')
    setAccOpeningYuan('0.00')
    setAccArchived(false)
    setDrawerOpen(true)
  }

  const openEditAccount = (acc: FinanceAccount) => {
    setEditingAccount(acc)
    setAccName(acc.name)
    setAccKind(acc.kind)
    setAccCurrency(acc.currency)
    setAccShopId(acc.shop_id ? String(acc.shop_id) : '')
    setAccOpeningYuan(fenToYuan(acc.opening_balance))
    setAccArchived(acc.archived)
    setDrawerOpen(true)
  }

  const handleSaveAccount = async () => {
    if (accSubmitting) return
    if (!accName.trim()) {
      toast('请输入账户名称', 'error')
      return
    }

    setAccSubmitting(true)
    try {
      if (editingAccount) {
        const body: Record<string, unknown> = {
          name: accName.trim(),
          kind: accKind,
          archived: accArchived
        }
        // 如果该账户还没有流水，允许改币种与店铺
        body.currency = accCurrency.trim().toUpperCase()
        body.shop_id = accShopId ? Number(accShopId) : null

        await api(`/api/finance/accounts/${editingAccount.id}`, {
          method: 'PATCH',
          body
        })
        toast('账户已更新', 'ok')
      } else {
        const { fen, error } = yuanToFen(accOpeningYuan)
        if (error) {
          toast(`期初余额不正确：${error}`, 'error')
          setAccSubmitting(false)
          return
        }
        await api('/api/finance/accounts', {
          method: 'POST',
          body: {
            name: accName.trim(),
            kind: accKind,
            currency: accCurrency.trim().toUpperCase(),
            shop_id: accShopId ? Number(accShopId) : null,
            opening_balance: fen
          }
        })
        toast('账户已创建', 'ok')
      }
      setDrawerOpen(false)
      reloadAccounts()
    } catch (e) {
      toast(errText(e), 'error')
    } finally {
      setAccSubmitting(false)
    }
  }

  const openCreateEntry = (defaultAccId?: number) => {
    const targetId = defaultAccId || selectedAccountId || (accountsData?.items[0]?.id ?? null)
    setEntryAccId(targetId)
    setEntryDirection('out')
    setEntryYuan('')
    setEntryCategory('')
    setEntryNote('')
    setEntryOccurred(financeNowInput())
    setEntryDrawerOpen(true)
  }

  const handleSaveEntry = async () => {
    if (entrySubmitting) return
    if (!entryAccId) {
      toast('请选择记账账户', 'error')
      return
    }
    const { fen, error } = yuanToFen(entryYuan)
    if (error || fen <= 0) {
      toast(error || '金额必须大于 0', 'error')
      return
    }
    if (!entryCategory.trim()) {
      toast('请输入收支类别', 'error')
      return
    }

    setEntrySubmitting(true)
    try {
      const selectedAcc = accountsData?.items.find(a => a.id === entryAccId)
      await api('/api/finance/entries', {
        method: 'POST',
        body: {
          account_id: entryAccId,
          direction: entryDirection,
          amount: fen,
          category: entryCategory.trim(),
          note: entryNote.trim(),
          occurred_at: entryOccurred ? financeInputUTC(entryOccurred) : undefined,
          shop_id: selectedAcc?.shop_id ?? null
        }
      })
      toast('记账成功', 'ok')
      setEntryDrawerOpen(false)
      reloadAccounts()
      reloadEntries()
    } catch (e) {
      toast(errText(e), 'error')
    } finally {
      setEntrySubmitting(false)
    }
  }

  return (
    <div className="finance-page">
      {/* 账户概览区域 */}
      <section className="panel">
        <div className="panel-head">
          <div className="panel-title">资金账户</div>
          <span className="grow" />
          {canEdit && (
            <button className="btn btn-primary btn-sm" onClick={openCreateAccount}>
              <Plus /> 新建账户
            </button>
          )}
        </div>

        <div className="finance-toolbar">
          <span className="finance-stat-label">状态：</span>
          <select value={archivedFilter} onChange={e => { setArchivedFilter(e.target.value as any); setAccountPage(1) }}>
            <option value="0">正常使用</option>
            <option value="1">仅已归档</option>
            <option value="all">全部账户</option>
          </select>

          <span className="finance-stat-label" style={{ marginLeft: 8 }}>归属店铺：</span>
          <select value={shopFilter} onChange={e => { setShopFilter(e.target.value); setAccountPage(1) }}>
            <option value="">全部店铺/公共</option>
            {me?.shops.map(s => (
              <option key={s.id} value={s.id}>{s.name}</option>
            ))}
          </select>
        </div>

        {accountsErr ? (
          <RecordState error={accountsErr} reload={reloadAccounts} />
        ) : !accountsData ? (
          <Loading />
        ) : accountsData.items.length === 0 ? (
          <Empty title="暂无资金账户" desc={canEdit ? '请点击右上角新建账户' : '暂无可见账户'} />
        ) : (
          <div className="finance-table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>账户名称</th>
                  <th>类型</th>
                  <th>币种</th>
                  <th>归属店铺</th>
                  <th className="finance-col-amount">期初余额</th>
                  <th className="finance-col-amount">当前可用余额</th>
                  <th>状态</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                {accountsData.items.map(acc => (
                  <tr
                    key={acc.id}
                    style={{ background: selectedAccountId === acc.id ? 'var(--primary-50)' : undefined }}
                  >
                    <td>
                      <span style={{ fontWeight: 600 }}>{acc.name}</span>
                    </td>
                    <td>{ACCOUNT_KIND_LABELS[acc.kind] || acc.kind}</td>
                    <td><span className="mono">{acc.currency}</span></td>
                    <td>{acc.shop_name || (acc.shop_id === null ? '公司公共' : `店铺 #${acc.shop_id}`)}</td>
                    <td className="finance-col-amount">{formatFen(acc.opening_balance, acc.currency)}</td>
                    <td className="finance-col-amount" style={{ fontWeight: 600 }}>
                      {formatFen(acc.balance, acc.currency)}
                    </td>
                    <td>
                      <Pill kind={acc.archived ? 'pill-neutral' : 'pill-success'}>
                        {acc.archived ? '已归档' : '正常'}
                      </Pill>
                    </td>
                    <td>
                      <div className="inline" style={{ gap: 4 }}>
                        <button
                          className="btn btn-ghost btn-sm"
                          onClick={() => setSelectedAccountId(selectedAccountId === acc.id ? null : acc.id)}
                          title="查看该账户流水"
                        >
                          <Search /> {selectedAccountId === acc.id ? '取消筛选' : '查流水'}
                        </button>
                        {canEdit && (
                          <>
                            <button className="btn btn-ghost btn-sm" onClick={() => openEditAccount(acc)}>
                              <Edit2 /> 编辑
                            </button>
                            {!acc.archived && (
                              <button className="btn btn-secondary btn-sm" onClick={() => openCreateEntry(acc.id)}>
                                记账
                              </button>
                            )}
                          </>
                        )}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        {accountsData && accountsData.total > 50 && (
          <div style={{ padding: '8px 16px' }}>
            <Pager total={accountsData.total} page={accountPage} setPage={setAccountPage} size={50} />
          </div>
        )}
      </section>

      {/* 账户流水明细区域 */}
      <section className="panel">
        <div className="panel-head">
          <div className="panel-title">
            收支流水明细
            {selectedAccountId && (
              <span style={{ fontSize: 12, fontWeight: 'normal', color: 'var(--text-3)', marginLeft: 8 }}>
                (已筛选特定账户 #{selectedAccountId})
              </span>
            )}
          </div>
          <span className="grow" />
          {canEdit && (
            <button className="btn btn-secondary btn-sm" onClick={() => openCreateEntry()}>
              <Plus /> 手动记账
            </button>
          )}
        </div>

        {entriesErr ? (
          <RecordState error={entriesErr} reload={reloadEntries} />
        ) : !entriesData ? (
          <Loading />
        ) : entriesData.items.length === 0 ? (
          <Empty title="暂无收支流水" desc="尚未发生资金收支记账" />
        ) : (
          <div className="finance-table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>发生时间</th>
                  <th>账户</th>
                  <th>方向</th>
                  <th className="finance-col-amount">发生金额</th>
                  <th>类别</th>
                  <th>来源</th>
                  <th>备注说明</th>
                  <th>记账时间</th>
                </tr>
              </thead>
              <tbody>
                {entriesData.items.map(ent => (
                  <tr key={ent.id}>
                    <td className="finance-col-date">
                      {financeTime(ent.occurred_at)}
                    </td>
                    <td>{ent.account_name}</td>
                    <td>
                      <Pill kind={ent.direction === 'in' ? 'pill-success' : 'pill-danger'}>
                        {ent.direction === 'in' ? '收入 (+)' : '支出 (-)'}
                      </Pill>
                    </td>
                    <td className="finance-col-amount" style={{ fontWeight: 600 }}>
                      <span style={{ color: ent.direction === 'in' ? 'var(--success-text)' : 'var(--danger-text)' }}>
                        {ent.direction === 'in' ? '+' : '-'}{formatFen(ent.amount, ent.currency)}
                      </span>
                    </td>
                    <td><span className="pill pill-neutral">{ent.category}</span></td>
                    <td>
                      <span className="help">
                        {ent.source_type === 'manual' ? '手动记账' :
                         ent.source_type === 'payment' ? `采购付款 #${ent.source_id}` :
                         ent.source_type === 'expense' ? `费用支出 #${ent.source_id}` :
                         ent.source_type}
                      </span>
                    </td>
                    <td style={{ maxWidth: 240, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      {ent.note || '—'}
                    </td>
                    <td className="finance-col-date">
                      {financeTime(ent.created_at)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        {entriesData && entriesData.total > 50 && (
          <div style={{ padding: '8px 16px' }}>
            <Pager total={entriesData.total} page={entryPage} setPage={setEntryPage} size={50} />
          </div>
        )}
      </section>

      {/* 账户新建/编辑抽屉 */}
      {drawerOpen && (
        <Drawer
          title={editingAccount ? '编辑资金账户' : '新建资金账户'}
          onClose={() => setDrawerOpen(false)}
          foot={
            <div className="inline" style={{ justifyContent: 'flex-end', gap: 8 }}>
              <button className="btn btn-ghost" onClick={() => setDrawerOpen(false)}>取消</button>
              <button className="btn btn-primary" disabled={accSubmitting} onClick={handleSaveAccount}>
                {accSubmitting ? '保存中…' : '保存'}
              </button>
            </div>
          }
        >
          <div className="finance-drawer-form">
            <Field label="账户名称" req>
              <input
                type="text"
                className="form-control"
                placeholder="例如：招商银行基本户、Shopee店铺钱包"
                value={accName}
                onChange={e => setAccName(e.target.value)}
              />
            </Field>

            <Field label="账户类型" req>
              <select
                className="form-control"
                value={accKind}
                onChange={e => setAccKind(e.target.value as AccountKind)}
              >
                <option value="bank">银行账户</option>
                <option value="alipay">支付宝</option>
                <option value="shopee">Shopee钱包</option>
                <option value="cash">现金</option>
              </select>
            </Field>

            <div className="finance-drawer-row">
              <Field label="账户币种" req hint={editingAccount ? '已有流水后不可修改' : undefined}>
                <input
                  type="text"
                  className="form-control mono"
                  placeholder="CNY"
                  value={accCurrency}
                  onChange={e => setAccCurrency(e.target.value.toUpperCase())}
                  maxLength={3}
                />
              </Field>

              <Field label="归属店铺" hint={isScopeAll ? '公共账户仅管理员/全范围角色可用' : undefined}>
                <select
                  className="form-control"
                  value={accShopId}
                  onChange={e => setAccShopId(e.target.value)}
                >
                  {isScopeAll && <option value="">公司公共（无特定店铺）</option>}
                  {me?.shops.map(s => (
                    <option key={s.id} value={s.id}>{s.name}</option>
                  ))}
                </select>
              </Field>
            </div>

            {!editingAccount && (
              <Field label="期初余额（元）" hint="仅在创建时设定，后续余额严格由流水计算产生">
                <input
                  type="number"
                  step="0.01"
                  className="form-control mono"
                  value={accOpeningYuan}
                  onChange={e => setAccOpeningYuan(e.target.value)}
                />
              </Field>
            )}

            {editingAccount && (
              <label className="form-field inline" style={{ gap: 8, alignItems: 'center', cursor: 'pointer' }}>
                <input
                  type="checkbox"
                  checked={accArchived}
                  onChange={e => setAccArchived(e.target.checked)}
                />
                <span className="label" style={{ marginBottom: 0 }}>归档此账户（归档后禁止新增记账）</span>
              </label>
            )}
          </div>
        </Drawer>
      )}

      {/* 手动记账抽屉 */}
      {entryDrawerOpen && (
        <Drawer
          title="手动登记收支流水"
          onClose={() => setEntryDrawerOpen(false)}
          foot={
            <div className="inline" style={{ justifyContent: 'flex-end', gap: 8 }}>
              <button className="btn btn-ghost" onClick={() => setEntryDrawerOpen(false)}>取消</button>
              <button className="btn btn-primary" disabled={entrySubmitting} onClick={handleSaveEntry}>
                {entrySubmitting ? '正在记账…' : '确认记账'}
              </button>
            </div>
          }
        >
          <div className="finance-drawer-form">
            <Field label="选择账户" req>
              <select
                className="form-control"
                value={entryAccId ?? ''}
                onChange={e => setEntryAccId(Number(e.target.value) || null)}
              >
                <option value="">请选择账户</option>
                {accountsData?.items.filter(a => !a.archived).map(a => (
                  <option key={a.id} value={a.id}>
                    {a.name} ({a.currency} 可用: {fenToYuan(a.balance)}元)
                  </option>
                ))}
              </select>
            </Field>

            <Field label="收支方向" req>
              <div className="inline" style={{ gap: 16 }}>
                <label className="inline" style={{ gap: 4, cursor: 'pointer' }}>
                  <input
                    type="radio"
                    name="direction"
                    value="out"
                    checked={entryDirection === 'out'}
                    onChange={() => setEntryDirection('out')}
                  />
                  <span>支出 (余额减少)</span>
                </label>
                <label className="inline" style={{ gap: 4, cursor: 'pointer' }}>
                  <input
                    type="radio"
                    name="direction"
                    value="in"
                    checked={entryDirection === 'in'}
                    onChange={() => setEntryDirection('in')}
                  />
                  <span>收入 (余额增加)</span>
                </label>
              </div>
            </Field>

            <Field label="金额（元）" req hint="系统将精确转为整数分保存，禁止透支">
              <input
                type="number"
                step="0.01"
                className="form-control mono"
                placeholder="0.00"
                value={entryYuan}
                onChange={e => setEntryYuan(e.target.value)}
              />
            </Field>

            <Field label="收支类别" req hint="例如：客户回款、投资注资、杂费">
              <input
                type="text"
                className="form-control"
                placeholder="请输入类别"
                value={entryCategory}
                onChange={e => setEntryCategory(e.target.value)}
              />
            </Field>

            <Field label="业务发生时间">
              <input
                type="datetime-local"
                className="form-control"
                value={entryOccurred}
                onChange={e => setEntryOccurred(e.target.value)}
              />
            </Field>

            <Field label="备注说明">
              <textarea
                className="form-control"
                rows={3}
                placeholder="填写相关单号或背景备注…"
                value={entryNote}
                onChange={e => setEntryNote(e.target.value)}
              />
            </Field>
          </div>
        </Drawer>
      )}
    </div>
  )
}
