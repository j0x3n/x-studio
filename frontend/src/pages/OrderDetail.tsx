import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { Pencil, Truck, Undo2 } from 'lucide-react'
import { api } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { fmtDate } from '../lib/format'
import { money, orderStatus, returnAction, type Order } from '../lib/commerce'
import { Topbar, Empty, Pill, Drawer, Field } from '../components/ui'
import { useWarehouses, useCostPerm } from '../components/InventoryForms'
import { DocumentPanel, RecordState, useRecord } from '../components/CommerceShared'
import { OrderForm, ShipForm, ReturnForm } from '../components/OrderForms'

export function OrderDetail() {
  const { id } = useParams()
  const { me, can } = useAuth()
  const cost = useCostPerm()
  const toast = useToast()
  const { data: o, error, reload } = useRecord<Order>(`/api/orders/${id}`)
  const { items: warehouses } = useWarehouses(true)
  const [drawer, setDrawer] = useState('')
  const [busy, setBusy] = useState(false)
  const manage = can('order.manage')
  if (!can('order.view')) return <><Topbar title="订单" /><Empty title="没有查看订单的权限" /></>
  if (!o) return <><Topbar crumbs={[<Link to="/orders">订单</Link>, '详情']} /><RecordState error={error} reload={reload} /></>
  const whName = (id: number) => warehouses?.find(w => w.id === id)?.name || `仓库 #${id}`
  const done = (message: string) => { toast(message, 'ok'); setDrawer(''); reload() }
  const cancel = async () => { setBusy(true); try { await api(`/api/orders/${o.id}/cancel`, { method: 'POST' }); done('订单已取消，库存占用已释放') } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) } }
  const refunded = o.returns.reduce((s, r) => s + r.refund_amount, 0)
  const allReturned = o.lines.length > 0 && o.lines.every(l => l.returned_qty === l.qty)
  return <>
    <Topbar crumbs={[<Link to="/orders">订单</Link>]} title={<span className="mono">{o.no}</span>} sub={<span className="inline"><Pill kind={orderStatus[o.status].pill}>{orderStatus[o.status].label}</Pill>{o.short && <span className="tag tag-red">缺货</span>}{allReturned && <span className="tag tag-gray">已全退</span>}</span>}>
      {manage && <button className="btn btn-secondary" onClick={() => setDrawer(o.status === 'paid' ? 'edit' : 'note')}><Pencil />{o.status === 'paid' ? '编辑订单' : '改备注'}</button>}
      {manage && o.status === 'paid' && <><button className="btn btn-danger" onClick={() => setDrawer('cancel')}>取消订单</button><button className="btn btn-primary" onClick={() => setDrawer('ship')}><Truck />发货</button></>}
      {manage && o.status === 'shipped' && <button className="btn btn-primary" onClick={() => setDrawer('return')}><Undo2 />登记售后</button>}
    </Topbar>
    <div className="scroll"><main className="content commerce-detail">
      <div className="stack">
        {o.short && <div className="commerce-notice">当前订单有缺货。已有库存已占用，不足部分需补货；发货时将再次检查整单库存。</div>}
        <DocumentPanel title="订单明细"><div className="table-wrap"><table className="table"><thead><tr><th>成品 SKU</th><th className="num">数量</th><th className="num">单价</th><th className="num">行实收</th><th className="num">已售后</th></tr></thead><tbody>{o.lines.map(l => <tr key={l.id}><td className="wrap"><Link className="link mono" to={`/inventory/items/${l.item_id}`}>{l.code}</Link><div className="cell-sub">{l.title}</div></td><td className="num mono">{l.qty}</td><td className="num mono t2">{money(l.price, o.currency)}</td><td className="num mono">{money(l.amount, o.currency)}</td><td className="num mono">{l.returned_qty || '—'}</td></tr>)}</tbody></table></div><div className="pager">商品 <span className="mono">{money(o.goods_amount, o.currency)}</span> · 运费 <span className="mono">{money(o.shipping_fee, o.currency)}</span> · 优惠 <span className="mono">{money(o.discount, o.currency)}</span><span className="spacer" />实收 <strong className="mono">{money(o.paid_amount, o.currency)}</strong></div></DocumentPanel>
        {o.status === 'shipped' && <DocumentPanel title="发货记录"><div className="commerce-record"><div className="inline"><strong>{o.carrier || '未填快递公司'}</strong><span className="mono">{o.tracking_no || '未填快递单号'}</span><span className="grow" /><span className="mono t3">{fmtDate(o.shipped_at, true)}</span></div></div><div className="table-wrap"><table className="table"><thead><tr><th>SKU</th><th>仓库</th><th>批次 / 等级</th><th className="num">发出</th>{cost && <th className="num">成本</th>}</tr></thead><tbody>{o.lines.flatMap(l => l.picks.map(p => <tr key={p.id}><td className="mono">{l.code}</td><td>{whName(p.warehouse_id)}</td><td>{p.batch_id ? <span className="mono">#{p.batch_id}</span> : '代发'}{p.grade && <> · {p.grade}</>}</td><td className="num mono">{p.qty}</td>{cost && <td className="num mono">{money(p.cost, p.currency)}</td>}</tr>))}</tbody></table></div></DocumentPanel>}
        <DocumentPanel title={`售后记录 · ${o.returns.length} 笔`}>{!o.returns.length ? <Empty title="暂无售后记录" /> : o.returns.map(r => <div key={r.id} className="commerce-record"><div className="inline"><strong>售后 #{r.id}</strong><span className="mono">退款 {money(r.refund_amount, o.currency)}</span><span className="grow" /><span className="mono t3">{fmtDate(r.created_at, true)}</span></div>{r.lines.map(l => <div className="inline t2" key={l.line_id}><span className="mono">{o.lines.find(x => x.id === l.line_id)?.code}</span><span>× {l.qty}</span><span>{returnAction[l.action]}</span>{l.action === 'restock' && l.warehouse_id && <span>→ {whName(l.warehouse_id)}</span>}</div>)}{!r.lines.length && <div className="help">仅退款，未处理商品数量</div>}{r.reason && <div className="commerce-pre t3">{r.reason}</div>}</div>)}</DocumentPanel>
      </div>
      <aside className="stack"><DocumentPanel title="收货信息"><dl className="kv commerce-kv"><dt>买家</dt><dd>{o.buyer_name || '—'}</dd><dt>手机</dt><dd className="mono">{o.buyer_phone || '—'}</dd><dt>地址</dt><dd>{o.address || '—'}</dd><dt>买家留言</dt><dd className="commerce-pre">{o.buyer_note || '—'}</dd></dl></DocumentPanel><DocumentPanel title="订单信息"><dl className="kv commerce-kv"><dt>店铺</dt><dd>{me?.shops.find(s => s.id === o.shop_id)?.name || `店铺 #${o.shop_id}`}</dd><dt>平台单号</dt><dd className="mono">{o.platform_no || '—'}</dd><dt>来源</dt><dd>{o.source === 'manual' ? '手工录单' : o.source}</dd><dt>付款</dt><dd className="mono">{fmtDate(o.paid_at, true)}</dd><dt>已退款</dt><dd className="mono">{money(refunded, o.currency)}</dd>{o.canceled_at && <><dt>取消</dt><dd className="mono">{fmtDate(o.canceled_at, true)}</dd></>}<dt>内部备注</dt><dd className="commerce-pre">{o.seller_note || '—'}</dd></dl></DocumentPanel></aside>
    </main></div>
    {drawer === 'edit' && <OrderForm value={o} onClose={() => setDrawer('')} onDone={() => done('订单已更新')} />}
    {drawer === 'ship' && <ShipForm value={o} onClose={() => setDrawer('')} onDone={() => done('订单已发货，库存已扣减')} />}
    {drawer === 'return' && <ReturnForm value={o} onClose={() => setDrawer('')} onDone={() => done('售后已登记')} />}
    {drawer === 'note' && <OrderNote value={o} onClose={() => setDrawer('')} onDone={() => done('备注已保存')} />}
    {drawer === 'cancel' && <Drawer title="取消订单" onClose={() => { if (!busy) setDrawer('') }} foot={<><button className="btn btn-secondary" disabled={busy} onClick={() => setDrawer('')}>返回</button><button className="btn btn-primary" disabled={busy} onClick={cancel}>确认取消</button></>}><p>取消 {o.no} 后会释放本单占用的库存，订单记录保留。</p></Drawer>}
  </>
}

function OrderNote({ value, onClose, onDone }: { value: Order; onClose: () => void; onDone: () => void }) {
  const [note, setNote] = useState(value.seller_note)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const save = async () => { setBusy(true); try { await api(`/api/orders/${value.id}`, { method: 'PATCH', body: { seller_note: note } }); onDone() } catch (e) { setError(errText(e)) } finally { setBusy(false) } }
  return <Drawer title="修改内部备注" onClose={() => { if (!busy) onClose() }} foot={<><button className="btn btn-secondary" disabled={busy} onClick={onClose}>取消</button><button className="btn btn-primary" disabled={busy} onClick={save}>保存备注</button></>}><Field label="内部备注"><textarea className="textarea" rows={5} maxLength={1000} value={note} onChange={e => setNote(e.target.value)} /></Field>{error && <div className="form-err" role="alert">{error}</div>}</Drawer>
}
