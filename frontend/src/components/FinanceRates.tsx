import { useState } from 'react'
import { Plus, Edit2 } from 'lucide-react'
import { api, qs } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { Drawer, Field, Loading } from './ui'
import { Pager, useRecord, RecordState } from './CommerceShared'
import {
  type FinanceRate,
  formatRateMicros,
  rateToMicros
} from '../lib/finance'

export function FinanceRates() {
  const { can } = useAuth()
  const toast = useToast()
  const canEdit = can('finance.edit')

  const [monthFilter, setMonthFilter] = useState(() => new Date().toISOString().slice(0, 7))
  const [currencyFilter, setCurrencyFilter] = useState('')
  const [page, setPage] = useState(1)

  const queryStr = qs({
    month: monthFilter || undefined,
    currency: currencyFilter || undefined,
    page,
    page_size: 50
  })

  const { data, error, reload } = useRecord<{ items: FinanceRate[]; total: number; cny_rate_micros: number }>(
    `/api/finance/rates${queryStr}`
  )

  // 设置汇率抽屉
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [rateMonth, setRateMonth] = useState(() => new Date().toISOString().slice(0, 7))
  const [rateCurrency, setRateCurrency] = useState('USD')
  const [rateValueStr, setRateValueStr] = useState('7.120000')
  const [submitting, setSubmitting] = useState(false)

  const openSetRate = (existing?: FinanceRate) => {
    if (existing) {
      setRateMonth(existing.month)
      setRateCurrency(existing.currency)
      setRateValueStr(formatRateMicros(existing.rate_micros))
    } else {
      setRateMonth(monthFilter || new Date().toISOString().slice(0, 7))
      setRateCurrency('USD')
      setRateValueStr('7.120000')
    }
    setDrawerOpen(true)
  }

  const handleSaveRate = async () => {
    if (submitting) return
    const cur = rateCurrency.trim().toUpperCase()
    if (!cur || cur.length !== 3) {
      toast('币种代码需为 3 位大写字母（如 USD、TWD）', 'error')
      return
    }

    if (cur === 'CNY') {
      toast('人民币（CNY）固定为 1.000000，无需额外配置', 'info')
      return
    }

    const { micros, error: err } = rateToMicros(rateValueStr)
    if (err || micros <= 0) {
      toast(err || '汇率必须为有效正数', 'error')
      return
    }

    setSubmitting(true)
    try {
      await api('/api/finance/rates', {
        method: 'PUT',
        body: {
          month: rateMonth,
          currency: cur,
          rate_micros: micros
        }
      })
      toast('月度汇率已保存', 'ok')
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
          <div className="panel-title">月度汇率换算配置</div>
          <span className="grow" />
          {canEdit && (
            <button className="btn btn-primary btn-sm" onClick={() => openSetRate()}>
              <Plus /> 配置汇率
            </button>
          )}
        </div>

        {/* 筛选工具栏 */}
        <div className="finance-toolbar">
          <span className="finance-stat-label">月份 (YYYY-MM)：</span>
          <input
            type="month"
            value={monthFilter}
            onChange={e => { setMonthFilter(e.target.value); setPage(1) }}
          />

          <span className="finance-stat-label" style={{ marginLeft: 8 }}>币种筛选：</span>
          <input
            type="text"
            placeholder="例如 USD"
            value={currencyFilter}
            onChange={e => { setCurrencyFilter(e.target.value.toUpperCase()); setPage(1) }}
            style={{ width: 100 }}
            maxLength={3}
          />
        </div>

        {error ? (
          <RecordState error={error} reload={reload} />
        ) : !data ? (
          <Loading />
        ) : (
          <div className="finance-table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>月份</th>
                  <th>币种</th>
                  <th className="finance-col-amount">兑换人民币标准汇率 (1 原币 = X CNY)</th>
                  <th>微单位基数 (rate_micros)</th>
                  <th>最近更新时间</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                {/* 固定的 CNY 基准行 */}
                {(!currencyFilter || currencyFilter === 'CNY') && (
                  <tr style={{ background: 'var(--surface-2)' }}>
                    <td><span className="mono">{monthFilter || '基础基准'}</span></td>
                    <td><span className="mono" style={{ fontWeight: 600 }}>CNY</span></td>
                    <td className="finance-col-amount" style={{ fontWeight: 600 }}>1.000000</td>
                    <td><span className="mono help">1000000 (恒定基准)</span></td>
                    <td className="finance-col-date">—</td>
                    <td><span className="help">系统基准</span></td>
                  </tr>
                )}

                {data.items.filter(item => item.currency !== 'CNY').map(rate => (
                  <tr key={`${rate.month}-${rate.currency}`}>
                    <td><span className="mono">{rate.month}</span></td>
                    <td><span className="mono" style={{ fontWeight: 600 }}>{rate.currency}</span></td>
                    <td className="finance-col-amount" style={{ fontWeight: 600, color: 'var(--primary-text)' }}>
                      {formatRateMicros(rate.rate_micros)}
                    </td>
                    <td><span className="mono help">{rate.rate_micros}</span></td>
                    <td className="finance-col-date">
                      {rate.updated_at ? rate.updated_at.slice(0, 19).replace('T', ' ') : '—'}
                    </td>
                    <td>
                      {canEdit ? (
                        <button className="btn btn-ghost btn-sm" onClick={() => openSetRate(rate)}>
                          <Edit2 /> 调整
                        </button>
                      ) : (
                        <span className="help">只读</span>
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

      {/* 汇率配置抽屉 */}
      {drawerOpen && (
        <Drawer
          title="配置月度折算汇率"
          onClose={() => setDrawerOpen(false)}
          foot={
            <div className="inline" style={{ justifyContent: 'flex-end', gap: 8 }}>
              <button className="btn btn-ghost" onClick={() => setDrawerOpen(false)}>取消</button>
              <button className="btn btn-primary" disabled={submitting} onClick={handleSaveRate}>
                {submitting ? '正在保存…' : '保存汇率'}
              </button>
            </div>
          }
        >
          <div className="finance-drawer-form">
            <Field label="核算月份" req hint="格式为 YYYY-MM，历史记录原币金额不会被改写">
              <input
                type="month"
                className="form-control"
                value={rateMonth}
                onChange={e => setRateMonth(e.target.value)}
              />
            </Field>

            <Field label="币种代码" req hint="三位标准货币大写代码，如 USD、TWD、EUR、JPY">
              <input
                type="text"
                className="form-control mono"
                placeholder="USD"
                value={rateCurrency}
                onChange={e => setRateCurrency(e.target.value.toUpperCase())}
                maxLength={3}
              />
            </Field>

            <Field label="换汇比率 (1 原币 = ? 人民币)" req hint="例如 1 美元兑换 7.12 人民币，请输入 7.12">
              <input
                type="number"
                step="0.000001"
                className="form-control mono"
                placeholder="7.120000"
                value={rateValueStr}
                onChange={e => setRateValueStr(e.target.value)}
              />
            </Field>

            <div className="finance-info-box">
              系统将以百万微单位（micros）精确存储，汇率调整仅在核算统计和多币种折算人民币时生效，不篡改原交易单据和账户原币流水。
            </div>
          </div>
        </Drawer>
      )}
    </div>
  )
}
