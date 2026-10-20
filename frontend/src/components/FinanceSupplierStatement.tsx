import { financeTime } from '../lib/finance'
import { useState, useEffect } from 'react'
import { Search } from 'lucide-react'
import { api, qs } from '../lib/api'
import { useToast, errText } from '../lib/toast'
import { Empty, Loading, Pill } from './ui'
import {
  type SupplierStatement,
  formatFen
} from '../lib/finance'

export function FinanceSupplierStatement() {
  const toast = useToast()

  // 供应商下拉列表
  const [suppliers, setSuppliers] = useState<{ id: number; name: string; archived: boolean }[]>([])
  const [selectedSupplierId, setSelectedSupplierId] = useState<string>('')
  const [fromDate, setFromDate] = useState<string>('')
  const [toDate, setToDate] = useState<string>('')

  const [loading, setLoading] = useState(false)
  const [statement, setStatement] = useState<SupplierStatement | null>(null)

  useEffect(() => {
    api<{ items: { id: number; name: string; archived: boolean }[] }>('/api/suppliers?archived=1&page_size=200')
      .then(r => {
        const list = r.items || []
        setSuppliers(list)
        if (list.length > 0 && !selectedSupplierId) {
          setSelectedSupplierId(String(list[0].id))
        }
      })
      .catch(() => {})
  }, [])

  const handleQuery = async () => {
    if (!selectedSupplierId) {
      toast('请先选择供应商', 'error')
      return
    }

    setLoading(true)
    try {
      const q = qs({
        from: fromDate || undefined,
        to: toDate || undefined
      })
      const res = await api<SupplierStatement>(`/api/finance/suppliers/${selectedSupplierId}/statement${q}`)
      setStatement(res)
    } catch (e) {
      toast(errText(e), 'error')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="finance-page">
      <section className="panel">
        <div className="panel-head">
          <div className="panel-title">供应商对账单</div>
        </div>

        {/* 筛选工具栏 */}
        <div className="finance-toolbar">
          <span className="finance-stat-label">选择供应商：</span>
          <select
            value={selectedSupplierId}
            onChange={e => setSelectedSupplierId(e.target.value)}
            style={{ minWidth: 180 }}
          >
            <option value="">请选择供应商</option>
            {suppliers.map(s => (
              <option key={s.id} value={s.id}>
                {s.name} {s.archived ? '(已归档)' : ''}
              </option>
            ))}
          </select>

          <span className="finance-stat-label" style={{ marginLeft: 8 }}>对账周期：</span>
          <input
            type="date"
            value={fromDate}
            onChange={e => setFromDate(e.target.value)}
            placeholder="开始日期"
          />
          <span style={{ color: 'var(--text-3)' }}>至</span>
          <input
            type="date"
            value={toDate}
            onChange={e => setToDate(e.target.value)}
            placeholder="结束日期"
          />

          <button className="btn btn-primary btn-sm" disabled={loading} onClick={handleQuery}>
            <Search /> {loading ? '查询中…' : '生成对账单'}
          </button>
        </div>

        {loading ? (
          <Loading />
        ) : !statement ? (
          <Empty title="请选择供应商并点击查询" desc="选择供应商和日期范围即可核算该期间的采购单与付款明细" />
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 16, padding: '16px 0' }}>
            {/* 供应商概况与货币统计 */}
            <div style={{ padding: '0 16px' }}>
              <div className="inline" style={{ gap: 8, marginBottom: 12 }}>
                <span style={{ fontSize: 16, fontWeight: 600 }}>{statement.supplier.name}</span>
                {statement.supplier.archived && <Pill kind="pill-neutral">已归档供应商</Pill>}
                <span className="help" style={{ marginLeft: 8 }}>
                  周期：{statement.from || '起始不限'} ~ {statement.to || '至今'}
                </span>
              </div>

              {statement.totals.length === 0 ? (
                <div className="finance-alert-box">该周期内未查询到该供应商的采购或付款往来。</div>
              ) : (
                <div className="finance-stat-grid">
                  {statement.totals.map(t => (
                    <div key={t.currency} className="finance-stat-card">
                      <div className="finance-stat-label">币种：<span className="mono">{t.currency}</span></div>
                      <div className="inline" style={{ justifyContent: 'space-between', fontSize: 13, marginTop: 4 }}>
                        <span style={{ color: 'var(--text-3)' }}>采购总额:</span>
                        <span className="mono">{formatFen(t.purchase_amount, t.currency)}</span>
                      </div>
                      <div className="inline" style={{ justifyContent: 'space-between', fontSize: 13 }}>
                        <span style={{ color: 'var(--text-3)' }}>拒收退货:</span>
                        <span className="mono" style={{ color: 'var(--danger-text)' }}>
                          -{formatFen(t.rejected_amount, t.currency)}
                        </span>
                      </div>
                      <div className="inline" style={{ justifyContent: 'space-between', fontSize: 13 }}>
                        <span style={{ color: 'var(--text-3)' }}>本期付款:</span>
                        <span className="mono" style={{ color: 'var(--success-text)' }}>
                          {formatFen(t.paid_in_period, t.currency)}
                        </span>
                      </div>
                      <div className="inline" style={{ justifyContent: 'space-between', fontSize: 13, borderTop: '1px solid var(--border)', paddingTop: 4, marginTop: 4 }}>
                        <span style={{ fontWeight: 600 }}>剩余应付:</span>
                        <span className="mono" style={{ fontWeight: 600, color: 'var(--warning-text)' }}>
                          {formatFen(t.purchase_payable, t.currency)}
                        </span>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>

            {/* 采购单据明细 */}
            <div style={{ padding: '0 16px' }}>
              <h3 style={{ fontSize: 14, fontWeight: 600, marginBottom: 8 }}>采购单据往来 ({statement.purchases.length} 笔)</h3>
              {statement.purchases.length === 0 ? (
                <div className="help" style={{ padding: '8px 0' }}>期间无采购单往来</div>
              ) : (
                <div className="finance-table-wrap">
                  <table className="table">
                    <thead>
                      <tr>
                        <th>采购单号</th>
                        <th>下单日期</th>
                        <th>单据状态</th>
                        <th>币种</th>
                        <th className="finance-col-amount">采购金额</th>
                        <th className="finance-col-amount">拒收退款</th>
                        <th className="finance-col-amount">已付款</th>
                        <th className="finance-col-amount">待付应付</th>
                      </tr>
                    </thead>
                    <tbody>
                      {statement.purchases.map(p => (
                        <tr key={p.purchase_id}>
                          <td><span className="mono" style={{ fontWeight: 600 }}>{p.no}</span></td>
                          <td className="finance-col-date">{p.ordered_at ? p.ordered_at.slice(0, 10) : '—'}</td>
                          <td>
                            <Pill kind={p.status === 'done' ? 'pill-success' : 'pill-info'}>
                              {p.status}
                            </Pill>
                          </td>
                          <td><span className="mono">{p.currency}</span></td>
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
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>

            {/* 付款记录明细 */}
            <div style={{ padding: '0 16px' }}>
              <h3 style={{ fontSize: 14, fontWeight: 600, marginBottom: 8 }}>财务付款往来 ({statement.payments.length} 笔)</h3>
              {statement.payments.length === 0 ? (
                <div className="help" style={{ padding: '8px 0' }}>期间无付款支出记录</div>
              ) : (
                <div className="finance-table-wrap">
                  <table className="table">
                    <thead>
                      <tr>
                        <th>付款时间</th>
                        <th>对应采购单</th>
                        <th>币种</th>
                        <th className="finance-col-amount">付款金额</th>
                        <th>付款流水号</th>
                        <th>备注说明</th>
                      </tr>
                    </thead>
                    <tbody>
                      {statement.payments.map(pay => (
                        <tr key={pay.id}>
                          <td className="finance-col-date">
                            {financeTime(pay.occurred_at)}
                          </td>
                          <td><span className="mono">{pay.purchase_no || `PO #${pay.purchase_id}`}</span></td>
                          <td><span className="mono">{pay.currency}</span></td>
                          <td className="finance-col-amount" style={{ fontWeight: 600, color: 'var(--success-text)' }}>
                            {formatFen(pay.amount, pay.currency)}
                          </td>
                          <td><span className="help">流水 #{pay.entry_id}</span></td>
                          <td>{pay.note || '—'}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
          </div>
        )}
      </section>
    </div>
  )
}
