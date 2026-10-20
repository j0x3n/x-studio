import { financeNowInput, financeInputUTC, financeTime } from '../lib/finance'
import { useState, useEffect } from 'react'
import { Plus } from 'lucide-react'
import { api, qs } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { Drawer, Field, Empty, Loading } from './ui'
import { Pager, useRecord, RecordState } from './CommerceShared'
import {
  type FinanceExpense,
  type FinanceAccount,
  formatFen,
  fenToYuan,
  yuanToFen,
  newRequestKey
} from '../lib/finance'

export function FinanceExpenses() {
  const { me, can, isAdmin } = useAuth()
  const toast = useToast()
  const canEdit = can('finance.edit')
  const isScopeAll = isAdmin || me?.role.scope === 'all'

  // 列表筛选
  const [shopFilter, setShopFilter] = useState<string>('')
  const [fromDate, setFromDate] = useState<string>('')
  const [toDate, setToDate] = useState<string>('')
  const [page, setPage] = useState<number>(1)

  // 资金账户列表（创建费用时需要指定扣款账户）
  const [accounts, setAccounts] = useState<FinanceAccount[]>([])
  useEffect(() => {
    api<{ items: FinanceAccount[] }>('/api/finance/accounts?archived=0&page_size=200')
      .then(r => setAccounts(r.items || []))
      .catch(() => {})
  }, [])

  const queryStr = qs({
    shop_id: shopFilter ? Number(shopFilter) : undefined,
    from: fromDate || undefined,
    to: toDate || undefined,
    page,
    page_size: 50
  })

  const { data, error, reload } = useRecord<{ items: FinanceExpense[]; total: number }>(
    `/api/finance/expenses${queryStr}`
  )

  // 记费用抽屉状态
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [expShopId, setExpShopId] = useState<string>('')
  const [expAccountId, setExpAccountId] = useState<number | null>(null)
  const [expYuan, setExpYuan] = useState<string>('')
  const [expCategory, setExpCategory] = useState<string>('')
  const [expNote, setExpNote] = useState<string>('')
  const [expOccurred, setExpOccurred] = useState<string>(() => financeNowInput())
  const [requestKey, setRequestKey] = useState<string>('')
  const [submitting, setSubmitting] = useState(false)

  // 根据选中的店铺筛选对应资金账户（费用店铺与扣款账户店铺必须一致）
  const filteredAccounts = accounts.filter(a => {
    if (expShopId === '') {
      // 公司公共费用 -> 必须使用公司公共账户
      return a.shop_id === null
    }
    return a.shop_id === Number(expShopId)
  })

  const handleFieldChange = () => {
    setRequestKey(newRequestKey())
  }

  const openCreateExpense = () => {
    setExpShopId('')
    setExpAccountId(null)
    setExpYuan('')
    setExpCategory('')
    setExpNote('')
    setExpOccurred(financeNowInput())
    setRequestKey(newRequestKey())
    setDrawerOpen(true)
  }

  const handleSaveExpense = async () => {
    if (submitting) return
    if (!expAccountId) {
      toast('请选择付款扣款账户', 'error')
      return
    }

    const { fen, error: err } = yuanToFen(expYuan)
    if (err || fen <= 0) {
      toast(err || '费用金额必须大于 0', 'error')
      return
    }

    if (!expCategory.trim()) {
      toast('请输入费用类别', 'error')
      return
    }

    setSubmitting(true)
    try {
      const res = await api<{ idempotent: boolean; expense: FinanceExpense }>('/api/finance/expenses', {
        method: 'POST',
        body: {
          shop_id: expShopId ? Number(expShopId) : null,
          account_id: expAccountId,
          amount: fen,
          category: expCategory.trim(),
          note: expNote.trim(),
          occurred_at: expOccurred ? financeInputUTC(expOccurred) : undefined,
          request_key: requestKey
        }
      })

      if (res.idempotent) {
        toast('检测到相同请求已成功记录（幂等），未重复扣款', 'info')
      } else {
        toast('费用记账成功，账户已同步支出', 'ok')
      }
      setDrawerOpen(false)
      reload()
    } catch (e) {
      toast(errText(e), 'error')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="finance-page">
      <section className="panel">
        <div className="panel-head">
          <div className="panel-title">日常经营费用记账</div>
          <span className="grow" />
          {canEdit && (
            <button className="btn btn-primary btn-sm" onClick={openCreateExpense}>
              <Plus /> 记一笔费用
            </button>
          )}
        </div>

        {/* 筛选工具栏 */}
        <div className="finance-toolbar">
          <span className="finance-stat-label">店铺范围：</span>
          <select value={shopFilter} onChange={e => { setShopFilter(e.target.value); setPage(1) }}>
            <option value="">全部店铺 / 公共</option>
            {me?.shops.map(s => (
              <option key={s.id} value={s.id}>{s.name}</option>
            ))}
          </select>

          <span className="finance-stat-label" style={{ marginLeft: 8 }}>发生日期：</span>
          <input
            type="date"
            value={fromDate}
            onChange={e => { setFromDate(e.target.value); setPage(1) }}
          />
          <span style={{ color: 'var(--text-3)' }}>至</span>
          <input
            type="date"
            value={toDate}
            onChange={e => { setToDate(e.target.value); setPage(1) }}
          />
        </div>

        {error ? (
          <RecordState error={error} reload={reload} />
        ) : !data ? (
          <Loading />
        ) : data.items.length === 0 ? (
          <Empty title="暂无费用记录" desc={canEdit ? '点击右上角“记一笔费用”录入经营杂费支出' : '当前没有费用记录'} />
        ) : (
          <div className="finance-table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>发生时间</th>
                  <th>归属店铺</th>
                  <th>扣款账户</th>
                  <th>费用类别</th>
                  <th className="finance-col-amount">支出金额</th>
                  <th>备注说明</th>
                  <th>流水凭证</th>
                  <th>登记时间</th>
                </tr>
              </thead>
              <tbody>
                {data.items.map(exp => (
                  <tr key={exp.id}>
                    <td className="finance-col-date">
                      {financeTime(exp.occurred_at)}
                    </td>
                    <td>{exp.shop_name}</td>
                    <td>
                      <span className="mono">账户 #{exp.account_id}</span>
                    </td>
                    <td>
                      <span className="pill pill-warning">{exp.category}</span>
                    </td>
                    <td className="finance-col-amount" style={{ fontWeight: 600, color: 'var(--danger-text)' }}>
                      -{formatFen(exp.amount, exp.currency)}
                    </td>
                    <td style={{ maxWidth: 260, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      {exp.note || '—'}
                    </td>
                    <td><span className="help">流水 #{exp.entry_id}</span></td>
                    <td className="finance-col-date">
                      {financeTime(exp.created_at)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        {data && data.total > 50 && (
          <div style={{ padding: '8px 16px' }}>
            <Pager total={data.total} page={page} setPage={setPage} size={50} />
          </div>
        )}
      </section>

      {/* 费用登记抽屉 */}
      {drawerOpen && (
        <Drawer
          title="记一笔经营费用"
          onClose={() => setDrawerOpen(false)}
          foot={
            <div className="inline" style={{ justifyContent: 'flex-end', gap: 8 }}>
              <button className="btn btn-ghost" onClick={() => setDrawerOpen(false)}>取消</button>
              <button className="btn btn-primary" disabled={submitting} onClick={handleSaveExpense}>
                {submitting ? '正在提交…' : '确认记账'}
              </button>
            </div>
          }
        >
          <div className="finance-drawer-form">
            <Field label="归属店铺" hint="费用店铺必须与扣款资金账户的归属一致">
              <select
                className="form-control"
                value={expShopId}
                onChange={e => {
                  setExpShopId(e.target.value)
                  setExpAccountId(null)
                  handleFieldChange()
                }}
              >
                {isScopeAll && <option value="">公司公共（非单一店铺）</option>}
                {me?.shops.map(s => (
                  <option key={s.id} value={s.id}>{s.name}</option>
                ))}
              </select>
            </Field>

            <Field label="扣款账户" req hint={filteredAccounts.length === 0 ? '该店铺暂无对应可用账户，请先在“账户与流水”中创建' : undefined}>
              <select
                className="form-control"
                value={expAccountId ?? ''}
                onChange={e => {
                  setExpAccountId(Number(e.target.value) || null)
                  handleFieldChange()
                }}
              >
                <option value="">请选择扣款账户</option>
                {filteredAccounts.map(a => (
                  <option key={a.id} value={a.id}>
                    {a.name} ({a.currency} 余额: {fenToYuan(a.balance)}元)
                  </option>
                ))}
              </select>
            </Field>

            <Field label="费用金额（元）" req hint="系统将记入该账户流水并扣减可用余额，不允许透支">
              <input
                type="number"
                step="0.01"
                className="form-control mono"
                placeholder="0.00"
                value={expYuan}
                onChange={e => { setExpYuan(e.target.value); handleFieldChange() }}
              />
            </Field>

            <Field label="费用类别" req hint="例如：耗材包装、快递费、平台推广投放、差旅杂费等">
              <input
                type="text"
                className="form-control"
                placeholder="请输入费用类别"
                value={expCategory}
                onChange={e => { setExpCategory(e.target.value); handleFieldChange() }}
              />
            </Field>

            <Field label="费用发生时间">
              <input
                type="datetime-local"
                className="form-control"
                value={expOccurred}
                onChange={e => { setExpOccurred(e.target.value); handleFieldChange() }}
              />
            </Field>

            <Field label="费用详细说明">
              <textarea
                className="form-control"
                rows={3}
                placeholder="备注经办人、发票流水或采购事项…"
                value={expNote}
                onChange={e => { setExpNote(e.target.value); handleFieldChange() }}
              />
            </Field>
          </div>
        </Drawer>
      )}
    </div>
  )
}
