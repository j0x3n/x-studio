import { useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { Plus, Search, AlertTriangle } from 'lucide-react'
import { qs, type Page } from '../lib/api'
import { useAuth } from '../lib/auth'
import { fmtDate } from '../lib/format'
import { money, orderStatus, type Order } from '../lib/commerce'
import { Topbar, Empty, Pill, Tabs } from '../components/ui'
import { Pager, RecordState, useRecord } from '../components/CommerceShared'
import { OrderForm } from '../components/OrderForms'

export function Orders() {
  const { me, can } = useAuth()
  const nav = useNavigate()
  const [sp, setSp] = useSearchParams()
  const [create, setCreate] = useState(false)
  const status = sp.get('status') || '', shop = sp.get('shop_id') || '', q = sp.get('q') || '', from = sp.get('from') || '', to = sp.get('to') || '', short = sp.get('short') === '1'
  const page = Number(sp.get('page')) || 1
  const set = (key: string, value: string) => { const n = new URLSearchParams(sp); if (value) n.set(key, value); else n.delete(key); if (key !== 'page') n.delete('page'); setSp(n, { replace: true }) }
  const { data, error, reload } = useRecord<Page<Order>>('/api/orders' + qs({ status, shop_id: shop, q, from, to, short: short ? 1 : undefined, page, page_size: 50 }))
  if (!can('order.view')) return <><Topbar title="订单" /><Empty title="没有查看订单的权限" /></>
  return <>
    <Topbar title="订单" sub="手工录单、发货与售后">{can('order.manage') && <button className="btn btn-primary" onClick={() => setCreate(true)}><Plus />手工录单</button>}</Topbar>
    <div className="scroll"><main className="content">
      <Tabs value={status} onChange={v => set('status', v)} items={[{ key: '', label: '全部' }, ...Object.entries(orderStatus).map(([key, v]) => ({ key, label: v.label }))]} />
      <div className="toolbar commerce-toolbar"><select className="input commerce-select" aria-label="店铺筛选" value={shop} onChange={e => set('shop_id', e.target.value)}><option value="">全部店铺</option>{me?.shops.map(s => <option key={s.id} value={s.id}>{s.name}</option>)}</select><input className="input mono commerce-date" aria-label="付款开始日期" type="date" value={from} onChange={e => set('from', e.target.value)} /><span className="t3">至</span><input className="input mono commerce-date" aria-label="付款结束日期" type="date" value={to} onChange={e => set('to', e.target.value)} /><button className={'btn btn-sm ' + (short ? 'btn-soft' : 'btn-ghost')} onClick={() => set('short', short ? '' : '1')}><AlertTriangle />只看缺货</button><span className="spacer" /><label className="field commerce-search"><Search /><input placeholder="单号、买家、手机或快递单号" value={q} onChange={e => set('q', e.target.value)} /></label></div>
      <section className="panel">
        {!data ? <RecordState error={error} reload={reload} /> : !data.items.length ? <Empty title={sp.size ? '没有符合条件的订单' : '还没有订单'} desc="录入已付款订单后自动占用库存，发货时选择仓库扣减。" /> : <div className="table-wrap"><table className="table"><thead><tr><th>订单号</th><th>店铺</th><th>买家</th><th>状态</th><th className="num">实收</th><th>付款时间</th><th>物流</th></tr></thead><tbody>{data.items.map(o => <tr key={o.id} className="clickable" onClick={() => nav(`/orders/${o.id}`)}><td><Link className="link mono" to={`/orders/${o.id}`}>{o.no}</Link>{o.platform_no && <div className="cell-sub mono">{o.platform_no}</div>}</td><td>{o.shop_name}</td><td><div>{o.buyer_name || '未填写'}</div><div className="cell-sub mono">{o.buyer_phone}</div></td><td><div className="inline"><Pill kind={orderStatus[o.status].pill}>{orderStatus[o.status].label}</Pill>{o.short && <span className="tag tag-red">缺货</span>}</div></td><td className="num mono">{money(o.paid_amount, o.currency)}</td><td className="t3 mono">{fmtDate(o.paid_at, true)}</td><td><div className="t2">{o.carrier || '—'}</div><div className="cell-sub mono">{o.tracking_no}</div></td></tr>)}</tbody></table></div>}
        {data && <Pager total={data.total} page={page} setPage={n => set('page', String(n))} />}
      </section>
    </main></div>
    {create && <OrderForm onClose={() => setCreate(false)} onDone={id => nav(`/orders/${id}`)} />}
  </>
}
