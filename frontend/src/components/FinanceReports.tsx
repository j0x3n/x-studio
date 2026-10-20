import { useState } from 'react'
import { Link } from 'react-router-dom'
import { qs } from '../lib/api'
import { useAuth } from '../lib/auth'
import { financeNowInput, formatFen } from '../lib/finance'
import { RecordState, useRecord } from './CommerceShared'
import { Empty, Tabs } from './ui'

type Money = {
  currency: string; revenue: number; unshipped_revenue: number; refund: number; platform_fee: number
  shipping_insurance: number; expense: number; cost: number | null; cost_hidden: boolean; cost_known: boolean
  cost_currency: string | null; cost_parts: { currency: string; amount: number }[]; unknown_cost_count: number
  profit: number | null; cny_profit: number | null; converted: boolean
}
type Rollup = { currencies: Money[]; cny_ready: boolean; cny_revenue: number | null; cny_unshipped_revenue: number | null; cny_refund: number | null; cny_cost: number | null; cny_expense: number | null; cny_profit: number | null }
type Report = {
  month: string; basis: string; cost_visible: boolean; includes_company_expense: boolean; item_expense_included: boolean
  missing_rates: { currency: string; reason: string }[]; totals: Rollup
  shops: (Rollup & { shop_id: number; shop_name: string })[]
  items: (Money & { shop_id: number; shop_name: string; item_id: number; product_id: number | null; code: string; title: string; product_name: string })[]
  company_expenses: { currency: string; amount: number; cny: number | null; converted: boolean }[]
  notes: { kind: string; shop_id: number; order_id?: number; order_no?: string; message: string }[]; notes_truncated: boolean
}

export function FinanceReports() {
  const { me, can } = useAuth()
  const [month, setMonth] = useState(() => financeNowInput().slice(0, 7))
  const [shop, setShop] = useState('')
  const [group, setGroup] = useState<'shops' | 'items' | 'currency'>('shops')
  const [allNotes, setAllNotes] = useState(false)
  const { data, error, reload } = useRecord<Report>('/api/finance/reports' + qs({ month, shop_id: shop }))
  const cost = data?.cost_visible ?? false
  const costs = (row: Money) => row.cost_hidden ? '—' : !row.cost_known ? '成本待核对' : row.cost_parts?.length ? row.cost_parts.map(p => formatFen(p.amount, p.currency)).join(' + ') : formatFen(row.cost, row.cost_currency || row.currency)
  const rows: (Money & { label: string; key: string; path?: string })[] = !data ? [] : group === 'items'
    ? data.items.map((row, i) => ({ ...row, label: `${row.code} ${row.title || row.product_name} · ${row.shop_name}`, key: `${row.shop_id}-${row.item_id}-${i}`, path: can('inventory.view') ? `/inventory/items/${row.item_id}` : row.product_id && can('product.view') ? `/products/${row.product_id}` : undefined }))
    : group === 'currency' ? data.totals.currencies.map(row => ({ ...row, key: row.currency, label: '全部汇总' }))
    : data.shops.flatMap(s => s.currencies.map(row => ({ ...row, key: `${s.shop_id}-${row.currency}`, label: s.shop_name })))
  return <div className="finance-page">
    <div className="finance-toolbar">
      <label className="inline">月份 <input className="input mono" type="month" value={month} onChange={e => setMonth(e.target.value)} /></label>
      <label className="inline">店铺 <select className="input" value={shop} onChange={e => setShop(e.target.value)}><option value="">全部授权店铺</option>{me?.shops.map(s => <option value={s.id} key={s.id}>{s.name}</option>)}</select></label>
      <button className="btn btn-secondary btn-sm" onClick={reload}>刷新报表</button>
    </div>
    {!data ? <RecordState error={error} reload={reload} /> : <>
      <div className="finance-stat-grid">
        <div className="finance-stat-card"><span className="finance-stat-label">销售收入 · 人民币</span><span className="finance-stat-value">{formatFen(data.totals.cny_revenue)}</span><span className="help">待发货收款 {formatFen(data.totals.cny_unshipped_revenue)}</span></div>
        <div className="finance-stat-card"><span className="finance-stat-label">退款 · 人民币</span><span className="finance-stat-value">{formatFen(data.totals.cny_refund)}</span></div>
        <div className="finance-stat-card"><span className="finance-stat-label">经营费用 · 人民币</span><span className="finance-stat-value">{formatFen(data.totals.cny_expense)}</span></div>
        {cost && <div className="finance-stat-card"><span className="finance-stat-label">利润 · 人民币</span><span className="finance-stat-value">{data.totals.cny_profit == null ? '待核对' : formatFen(data.totals.cny_profit)}</span><span className="help">待发货收款不计入利润</span></div>}
      </div>
      {data.missing_rates.length > 0 && <div className="finance-alert-box">缺少当月汇率：{data.missing_rates.map(r => r.currency).join('、')}。请在「月度汇率」补齐；未换算的金额和利润保留为空。</div>}
      <details className="panel panel-pad"><summary className="cell-title">报表口径与分摊说明</summary><p className="help">{data.basis}</p>{!data.item_expense_included && <p className="help">物料维度未分摊店铺及公司公共费用；店铺和汇总维度按实际费用计入。</p>}</details>
      <Tabs value={group} onChange={setGroup} items={[{ key: 'shops', label: '按店铺' }, { key: 'items', label: '按款 / 物料' }, { key: 'currency', label: '原币汇总' }]} />
      <section className="panel">
        {!rows.length ? <Empty title="这个月没有可汇总的业务" /> : <div className="finance-table-wrap"><table className="table"><thead><tr><th>{group === 'items' ? '款 / 物料' : '店铺'}</th><th>原币</th><th className="finance-col-amount">收入</th><th className="finance-col-amount">待发货收款</th><th className="finance-col-amount">退款</th>{cost && <th className="finance-col-amount">实际出库成本</th>}<th className="finance-col-amount">平台费用 / 运费险</th><th className="finance-col-amount">经营费用</th>{cost && <><th className="finance-col-amount">原币利润</th><th className="finance-col-amount">人民币利润</th></>}</tr></thead><tbody>{rows.map(row => <tr key={row.key}><td className="wrap">{row.path ? <Link to={row.path}>{row.label}</Link> : row.label}{row.unknown_cost_count > 0 && <div className="help">{row.unknown_cost_count} 项成本待核对</div>}</td><td className="mono">{row.currency}</td><td className="finance-col-amount">{formatFen(row.revenue, row.currency)}</td><td className="finance-col-amount">{formatFen(row.unshipped_revenue, row.currency)}</td><td className="finance-col-amount">{formatFen(row.refund, row.currency)}</td>{cost && <td className="finance-col-amount">{costs(row)}</td>}<td className="finance-col-amount">{formatFen(row.platform_fee + row.shipping_insurance, row.currency)}</td><td className="finance-col-amount">{formatFen(row.expense, row.currency)}</td>{cost && <><td className="finance-col-amount">{formatFen(row.profit, row.currency)}</td><td className="finance-col-amount">{formatFen(row.cny_profit)}</td></>}</tr>)}</tbody></table></div>}
      </section>
      {data.company_expenses.length > 0 && <section className="panel panel-pad"><div className="panel-title">公司公共费用</div><div className="inline" style={{ flexWrap: 'wrap' }}>{data.company_expenses.map(e => <span className="chip" key={e.currency}>{formatFen(e.amount, e.currency)}{e.currency !== 'CNY' && ` · 人民币 ${formatFen(e.cny)}`}</span>)}</div><p className="help">仅全部店铺的公司汇总计入，不摊到某个款。</p></section>}
      {data.notes.length > 0 && <section className="panel panel-pad"><div className="inline"><span className="panel-title">待核对事项 · {data.notes.length}{data.notes_truncated ? '+' : ''}</span><span className="grow" /><button className="btn btn-ghost btn-sm" onClick={() => setAllNotes(!allNotes)}>{allNotes ? '收起' : '展开全部'}</button></div><ul className="help">{data.notes.slice(0, allNotes ? data.notes.length : 8).map((n, i) => <li key={i}>{n.order_id && can('order.view') ? <Link to={`/orders/${n.order_id}`}>{n.order_no || `订单 #${n.order_id}`}</Link> : n.order_no} {n.message}</li>)}</ul></section>}
    </>}
  </div>
}
