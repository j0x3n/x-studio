import { useState, useEffect } from 'react'
import { Search, CreditCard } from 'lucide-react'
import { api, qs } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { Drawer, Field, Pill, Empty, Loading } from './ui'
import { Pager, useRecord, RecordState } from './CommerceShared'
import {
  type FinancePayable,
  type FinanceAccount,
  formatFen,
  fenToYuan,
  yuanToFen,
  newRequestKey
} from '../lib/finance'

export function FinancePayables() {
  const { can } = useAuth()
  const toast = useToast()
  const canEdit = can('finance.edit')

  // 筛选与分页
  const [supplierId, setSupplierId] = useState<string>('')
  const [currency, setCurrency] = useState<string>('')
  const [searchNo, setSearchNo] = useState<string>('')
  const [unpaidOnly, setUnpaidOnly] = useState<boolean>(true)
  const [page, setPage] = useState<number>(1)

  // 供应商下拉列表
  const [suppliers, setSuppliers] = useState<{ id: number; name: string }[]>([])
  useEffect(() => {
    api<{ items: { id: number; name: string }[] }>('/api/suppliers?archived=1&page_size=200')
      .then(r => setSuppliers(r.items || []))
      .catch(() => {})
  }, [])

  // 资金账户列表（用于付款账户下拉，需按币种过滤）
  const [accounts, setAccounts] = useState<FinanceAccount[]>([])
  useEffect(() => {
    api<{ items: FinanceAccount[] }>('/api/finance/accounts?archived=0&page_size=200')
      .then(r => setAccounts(r.items || []))
      .catch(() => {})
  }, [])

  const queryStr = qs({
    supplier_id: supplierId ? Number(supplierId) : undefined,
    currency: currency || undefined,
    q: searchNo.trim() || undefined,
    unpaid: unpaidOnly ? 1 : undefined,
    page,
    page_size: 50
  })

  const { data, error, reload } = useRecord<{ items: FinancePayable[]; total: number }>(
    `/api/finance/payables${queryStr}`
  )

  // 付款抽屉状态
  const [paymentDrawerOpen, setPaymentDrawerOpen] = useState(false)
  const [selectedPayable, setSelectedPayable] = useState<FinancePayable | null>(null)
  const [payAccountId, setPayAccountId] = useState<number | null>(null)
  const [payAmountYuan, setPayAmountYuan] = useState<string>('')
  const [payNote, setPayNote] = useState<string>('')
  const [requestKey, setRequestKey] = useState<string>('')
  const [submitting, setSubmitting] = useState(false)

  // 跟踪表单修改以更新请求幂等键
  const handleAmountChange = (val: string) => {
    setPayAmountYuan(val)
    setRequestKey(newRequestKey())
  }

  const handleAccountChange = (accId: number | null) => {
    setPayAccountId(accId)
    setRequestKey(newRequestKey())
  }

  const handleNoteChange = (val: string) => {
    setPayNote(val)
    setRequestKey(newRequestKey())
  }

  const openPaymentDrawer = (payable: FinancePayable) => {
    setSelectedPayable(payable)
    // 默认选择币种匹配的第一个可用账户
    const matchingAccounts = accounts.filter(a => a.currency === payable.currency && !a.archived)
    setPayAccountId(matchingAccounts[0]?.id ?? null)
    setPayAmountYuan(fenToYuan(payable.payable))
    setPayNote(`采购单 ${payable.no} 付款`)
    setRequestKey(newRequestKey())
    setPaymentDrawerOpen(true)
  }

  const handleConfirmPayment = async () => {
    if (submitting || !selectedPayable) return
    if (!payAccountId) {
      toast('请选择付款账户', 'error')
      return
    }

    const { fen, error: err } = yuanToFen(payAmountYuan)
    if (err || fen <= 0) {
      toast(err || '付款金额必须大于 0', 'error')
      return
    }

    if (fen > selectedPayable.payable) {
      toast(`付款金额不能超过剩余待付金额 (${formatFen(selectedPayable.payable, selectedPayable.currency)})`, 'error')
      return
    }

    setSubmitting(true)
    try {
      const res = await api<{ idempotent: boolean; payable: number }>(
        `/api/finance/payables/${selectedPayable.purchase_id}/payments`,
        {
          method: 'POST',
          body: {
            account_id: payAccountId,
            amount: fen,
            note: payNote.trim(),
            request_key: requestKey
          }
        }
      )

      if (res.idempotent) {
        toast('检测到相同请求已成功记录（幂等），未重复扣款', 'info')
      } else {
        toast('付款成功，账户已记录支出', 'ok')
      }
      setPaymentDrawerOpen(false)
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
          <div className="panel-title">采购应付账款</div>
          <span className="grow" />
          <div className="inline" style={{ gap: 8 }}>
            <span className="help">只读采购单据减退货已核算数据，公司内部共享</span>
          </div>
        </div>

        {/* 筛选工具栏 */}
        <div className="finance-toolbar">
          <span className="finance-stat-label">供应商：</span>
          <select value={supplierId} onChange={e => { setSupplierId(e.target.value); setPage(1) }}>
            <option value="">全部供应商</option>
            {suppliers.map(s => (
              <option key={s.id} value={s.id}>{s.name}</option>
            ))}
          </select>

          <span className="finance-stat-label" style={{ marginLeft: 8 }}>币种：</span>
          <select value={currency} onChange={e => { setCurrency(e.target.value); setPage(1) }}>
            <option value="">全部币种</option>
            <option value="CNY">CNY 人民币</option>
            <option value="USD">USD 美元</option>
            <option value="TWD">TWD 新台币</option>
          </select>

          <span className="finance-stat-label" style={{ marginLeft: 8 }}>单号搜索：</span>
          <div className="inline" style={{ gap: 4 }}>
            <input
              type="text"
              placeholder="采购单号 PO…"
              value={searchNo}
              onChange={e => setSearchNo(e.target.value)}
              onKeyDown={e => { if (e.key === 'Enter') setPage(1) }}
              style={{ width: 140 }}
            />
            <button className="btn btn-ghost btn-sm" onClick={() => setPage(1)}>
              <Search /> 筛选
            </button>
          </div>

          <label className="inline" style={{ gap: 4, marginLeft: 12, cursor: 'pointer' }}>
            <input
              type="checkbox"
              checked={unpaidOnly}
              onChange={e => { setUnpaidOnly(e.target.checked); setPage(1) }}
            />
            <span style={{ fontSize: 13 }}>仅看未结清</span>
          </label>
        </div>

        {error ? (
          <RecordState error={error} reload={reload} />
        ) : !data ? (
          <Loading />
        ) : data.items.length === 0 ? (
          <Empty title="暂无应付记录" desc={unpaidOnly ? '当前没有未结清的采购单' : '没有找到符合条件的采购单'} />
        ) : (
          <div className="finance-table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>采购单号</th>
                  <th>供应商</th>
                  <th>单据状态</th>
                  <th>下单时间</th>
                  <th className="finance-col-amount">采购总额</th>
                  <th className="finance-col-amount">拒收/退货</th>
                  <th className="finance-col-amount">已付款</th>
                  <th className="finance-col-amount">待付应付</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                {data.items.map(p => (
                  <tr key={p.purchase_id}>
                    <td>
                      <span className="mono" style={{ fontWeight: 600 }}>{p.no}</span>
                    </td>
                    <td>{p.supplier_name}</td>
                    <td>
                      <Pill kind={p.status === 'done' ? 'pill-success' : 'pill-info'}>
                        {p.status === 'ordered' ? '已下单' :
                         p.status === 'partial' ? '部分到货' :
                         p.status === 'received' ? '全部到货' :
                         p.status === 'done' ? '已完成' : p.status}
                      </Pill>
                    </td>
                    <td className="finance-col-date">
                      {p.ordered_at ? p.ordered_at.slice(0, 10) : '—'}
                    </td>
                    <td className="finance-col-amount">{formatFen(p.order_amount, p.currency)}</td>
                    <td className="finance-col-amount">
                      {p.rejected_amount > 0 ? (
                        <span style={{ color: 'var(--danger-text)' }}>
                          -{formatFen(p.rejected_amount, p.currency)}
                        </span>
                      ) : '—'}
                    </td>
                    <td className="finance-col-amount">{formatFen(p.paid_amount, p.currency)}</td>
                    <td className="finance-col-amount" style={{ fontWeight: 600 }}>
                      <span style={{ color: p.payable > 0 ? 'var(--warning-text)' : 'var(--text-3)' }}>
                        {formatFen(p.payable, p.currency)}
                      </span>
                    </td>
                    <td>
                      {canEdit && p.payable > 0 ? (
                        <button className="btn btn-secondary btn-sm" onClick={() => openPaymentDrawer(p)}>
                          <CreditCard /> 付款
                        </button>
                      ) : (
                        <span className="help">{p.payable < 0 ? '待供应商退款' : p.payable === 0 ? '已付清' : '只读'}</span>
                      )}
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

      {/* 付款抽屉 */}
      {paymentDrawerOpen && selectedPayable && (
        <Drawer
          title={`采购单付款 — ${selectedPayable.no}`}
          onClose={() => setPaymentDrawerOpen(false)}
          foot={
            <div className="inline" style={{ justifyContent: 'flex-end', gap: 8 }}>
              <button className="btn btn-ghost" onClick={() => setPaymentDrawerOpen(false)}>取消</button>
              <button className="btn btn-primary" disabled={submitting} onClick={handleConfirmPayment}>
                {submitting ? '正在提交…' : '确认支付'}
              </button>
            </div>
          }
        >
          <div className="finance-drawer-form">
            <div className="finance-stat-card" style={{ background: 'var(--surface-2)' }}>
              <div className="finance-stat-label">供应商：{selectedPayable.supplier_name}</div>
              <div className="inline" style={{ justifyContent: 'space-between', marginTop: 4 }}>
                <span style={{ fontSize: 13, color: 'var(--text-2)' }}>剩余应付金额：</span>
                <span className="mono" style={{ fontSize: 16, fontWeight: 600, color: 'var(--warning-text)' }}>
                  {formatFen(selectedPayable.payable, selectedPayable.currency)}
                </span>
              </div>
            </div>

            <Field label="付款资金账户" req hint={`仅支持选择 ${selectedPayable.currency} 币种的正常状态账户`}>
              <select
                className="form-control"
                value={payAccountId ?? ''}
                onChange={e => handleAccountChange(Number(e.target.value) || null)}
              >
                <option value="">请选择出资账户</option>
                {accounts
                  .filter(a => a.currency === selectedPayable.currency && !a.archived)
                  .map(a => (
                    <option key={a.id} value={a.id}>
                      {a.name} ({a.shop_name || '公共'}) 可用: {fenToYuan(a.balance)}元
                    </option>
                  ))}
              </select>
            </Field>

            <Field label="本次付款金额（元）" req hint="支持部分付款，传值不能超过剩余待付应付金额">
              <input
                type="number"
                step="0.01"
                className="form-control mono"
                placeholder="0.00"
                value={payAmountYuan}
                onChange={e => handleAmountChange(e.target.value)}
              />
            </Field>

            <Field label="付款备注">
              <input
                type="text"
                className="form-control"
                placeholder="网银转账流水号、银行支票号等…"
                value={payNote}
                onChange={e => handleNoteChange(e.target.value)}
              />
            </Field>
          </div>
        </Drawer>
      )}
    </div>
  )
}
