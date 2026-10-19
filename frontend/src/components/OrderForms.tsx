import { useEffect, useState } from 'react'
import { Plus, X } from 'lucide-react'
import { api, qs } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { fenToInput, toFen, type InvItem, type Batch } from '../lib/inventory'
import { chinaDate, money, returnAction, type Order, type ReturnAction } from '../lib/commerce'
import { Drawer, Field } from './ui'
import { ItemPicker, useWarehouses } from './InventoryForms'

type Line = { item: InvItem | null; item_id: number; qty: string; price: string; amount: string }
const fresh = (): Line => ({ item: null, item_id: 0, qty: '1', price: '', amount: '' })
export function OrderForm({ value, onClose, onDone }: { value?: Order; onClose: () => void; onDone: (id: number) => void }) {
  const { me, can } = useAuth()
  const toast = useToast()
  const shops = (me?.shops || []).filter(s => !s.archived)
  const [shop, setShop] = useState(value?.shop_id || shops[0]?.id || 0)
  const [platform, setPlatform] = useState(value?.platform_no || '')
  const [buyer, setBuyer] = useState(value?.buyer_name || '')
  const [phone, setPhone] = useState(value?.buyer_phone || '')
  const [address, setAddress] = useState(value?.address || '')
  const [buyerNote, setBuyerNote] = useState(value?.buyer_note || '')
  const [note, setNote] = useState(value?.seller_note || '')
  const [currency, setCurrency] = useState(value?.currency || 'CNY')
  const [shipping, setShipping] = useState(fenToInput(value?.shipping_fee ?? 0))
  const [discount, setDiscount] = useState(fenToInput(value?.discount ?? 0))
  const [paid, setPaid] = useState(value ? fenToInput(value.paid_amount) : '')
  const [date, setDate] = useState(chinaDate(value?.paid_at))
  const [allowShort, setAllowShort] = useState(value?.short || false)
  const [lines, setLines] = useState<Line[]>(value ? value.lines.map(l => ({ item: { id: l.item_id, code: l.code, name: l.title, kind: 'sku' } as InvItem, item_id: l.item_id, qty: String(l.qty), price: fenToInput(l.price), amount: fenToInput(l.amount) })) : [fresh()])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const upd = (i: number, patch: Partial<Line>) => setLines(ls => ls.map((l, j) => i === j ? { ...l, ...patch } : l))
  const goods = lines.reduce((s, l) => s + (l.amount === '' ? toFen(l.price) * Number(l.qty) : toFen(l.amount)), 0)
  const expected = goods + toFen(shipping) - toFen(discount)
  const valid = shop > 0 && expected >= 0 && Number(shipping) >= 0 && Number(discount) >= 0 && (paid === '' || Number(paid) >= 0) && lines.length > 0 && lines.every(l => l.item_id && Number.isSafeInteger(Number(l.qty)) && Number(l.qty) > 0 && l.price.trim() !== '' && Number(l.price) >= 0 && (l.amount === '' || Number(l.amount) >= 0))
  const save = async () => {
    if (!valid) return
    setBusy(true); setError('')
    try {
      const r = await api<{ id: number }>('/api/orders' + (value ? `/${value.id}` : ''), { method: value ? 'PATCH' : 'POST', body: { ...(value ? {} : { shop_id: shop }), platform_no: platform, buyer_name: buyer, buyer_phone: phone, address, buyer_note: buyerNote, seller_note: note, currency, shipping_fee: toFen(shipping), discount: toFen(discount), paid_amount: paid === '' ? null : toFen(paid), paid_at: value && date === chinaDate(value.paid_at) ? value.paid_at : date, allow_short: allowShort, lines: lines.map(l => ({ item_id: l.item_id, qty: Number(l.qty), price: toFen(l.price), amount: l.amount === '' ? null : toFen(l.amount) })) } })
      toast(value ? '订单已保存' : '订单已录入', 'ok'); onDone(r.id)
    } catch (e) { setError(errText(e)) } finally { setBusy(false) }
  }
  return <Drawer title={value ? `编辑 ${value.no}` : '手工录入已付款订单'} width={920} onClose={() => { if (!busy) onClose() }} foot={<><span className="mono t2">实收 {money(paid === '' ? expected : toFen(paid), currency)}</span><span className="grow" /><button className="btn btn-secondary" disabled={busy} onClick={onClose}>取消</button><button className="btn btn-primary" disabled={busy || !valid} onClick={save}>{busy ? '保存中…' : value ? '保存订单' : '录单并占库存'}</button></>}>
    <div className="form-row"><Field label="店铺" req><select className="input" disabled={!!value} value={shop} onChange={e => setShop(Number(e.target.value))}><option value={0}>选择店铺</option>{value && !shops.some(s => s.id === value.shop_id) && <option value={value.shop_id}>原店铺 #{value.shop_id}</option>}{shops.map(s => <option key={s.id} value={s.id}>{s.name}</option>)}</select></Field><Field label="平台订单号"><input className="input mono" maxLength={100} value={platform} onChange={e => setPlatform(e.target.value)} placeholder="选填，同店铺内不能重复" /></Field></div>
    <div className="form-row"><Field label="买家 / 收货人"><input className="input" maxLength={100} value={buyer} onChange={e => setBuyer(e.target.value)} /></Field><Field label="手机号"><input className="input mono" maxLength={100} value={phone} onChange={e => setPhone(e.target.value)} /></Field></div><Field label="收货地址"><input className="input" maxLength={1000} value={address} onChange={e => setAddress(e.target.value)} /></Field>
    {!can('inventory.view') && <div className="commerce-notice">当前账号没有查看物料的权限，请管理员补充「看库存和物料」权限后录单。</div>}
    <div className="commerce-lines commerce-sales"><div className="commerce-line-head"><span>成品 SKU</span><span>数量</span><span>单价</span><span>行实收</span><span /></div>{lines.map((l, i) => <div className="commerce-line" key={i}><ItemPicker kind="sku" value={l.item} onChange={it => upd(i, { item: it, item_id: it?.id || 0 })} /><input className="input mono" type="number" min={1} step={1} aria-label={`第${i + 1}行数量`} value={l.qty} onChange={e => upd(i, { qty: e.target.value, amount: '' })} /><input className="input mono" type="number" min={0} step="0.01" aria-label={`第${i + 1}行单价`} value={l.price} onChange={e => upd(i, { price: e.target.value, amount: '' })} /><input className="input mono" type="number" min={0} step="0.01" aria-label={`第${i + 1}行实收`} placeholder={fenToInput(toFen(l.price) * Number(l.qty))} value={l.amount} onChange={e => upd(i, { amount: e.target.value })} /><button className="btn btn-ghost btn-icon" title="移除明细" disabled={lines.length === 1} onClick={() => setLines(ls => ls.filter((_, j) => i !== j))}><X /></button></div>)}</div>
    <button className="btn btn-secondary commerce-add" disabled={lines.length >= 200} onClick={() => setLines(ls => [...ls, fresh()])}><Plus />添加 SKU</button><div className="help">只支持成品 SKU。行实收留空时按单价 × 数量计算。</div>
    <div className="commerce-form-four"><Field label="币种"><select className="input" value={currency} onChange={e => setCurrency(e.target.value)}>{Array.from(new Set(['CNY', 'TWD', 'USD', currency])).map(c => <option key={c}>{c}</option>)}</select></Field><Field label="运费"><input className="input mono" type="number" min={0} step="0.01" value={shipping} onChange={e => setShipping(e.target.value)} /></Field><Field label="整单优惠"><input className="input mono" type="number" min={0} step="0.01" value={discount} onChange={e => setDiscount(e.target.value)} /></Field><Field label="实收金额" hint="留空自动计算"><input className="input mono" type="number" min={0} step="0.01" placeholder={fenToInput(expected)} value={paid} onChange={e => setPaid(e.target.value)} /></Field></div>
    <Field label="付款日期"><input className="input mono" type="date" value={date} onChange={e => setDate(e.target.value)} /></Field><div className="form-row"><Field label="买家留言"><textarea className="textarea" rows={2} maxLength={1000} value={buyerNote} onChange={e => setBuyerNote(e.target.value)} /></Field><Field label="内部备注"><textarea className="textarea" rows={2} maxLength={1000} value={note} onChange={e => setNote(e.target.value)} /></Field></div>
    <label className="inline"><input className="check" type="checkbox" checked={allowShort} onChange={e => setAllowShort(e.target.checked)} />库存不足时仍录入<span className="t3 fs-12">不足部分不占用，订单标记缺货，补货后才能发出</span></label>{error && <div className="form-err" role="alert">{error}</div>}
  </Drawer>
}

function ShipLine({ item, index, warehouse, batch, onChange, warehouses }: { item: Order['lines'][number]; index: number; warehouse: number; batch: number; onChange: (w: number, b: number) => void; warehouses: { id: number; name: string; type: string }[] }) {
  const [batches, setBatches] = useState<Batch[]>([])
  useEffect(() => {
    let active = true
    setBatches([])
    if (warehouse && warehouses.find(w => w.id === warehouse)?.type !== 'dropship') api<Batch[] | { items: Batch[] }>('/api/inventory/batches' + qs({ item_id: item.item_id, warehouse_id: warehouse })).then(r => { if (active) setBatches(Array.isArray(r) ? r : r.items) }).catch(() => {})
    return () => { active = false }
  }, [item.item_id, warehouse]) // eslint-disable-line react-hooks/exhaustive-deps
  const dropship = warehouses.find(w => w.id === warehouse)?.type === 'dropship'
  return <section className="commerce-receipt"><div className="inline"><span className="mono">{item.code}</span><strong>{item.title}</strong><span className="grow" /><span className="mono">× {item.qty}</span></div><div className="form-row"><Field label="发货仓库"><select className="input" aria-label={`第${index + 1}行发货仓库`} value={warehouse} onChange={e => onChange(Number(e.target.value), 0)}><option value={0}>选择仓库</option>{warehouses.map(w => <option key={w.id} value={w.id}>{w.name}</option>)}</select></Field><Field label="出库批次"><select className="input" disabled={dropship} value={batch} onChange={e => onChange(warehouse, Number(e.target.value))}><option value={0}>{dropship ? '代发，无批次' : '先进先出'}</option>{batches.map(b => <option key={b.id} value={b.id}>#{b.id} · {b.grade || '无等级'} · 剩 {b.qty_left}</option>)}</select></Field></div></section>
}

export function ShipForm({ value, onClose, onDone }: { value: Order; onClose: () => void; onDone: () => void }) {
  const { items } = useWarehouses()
  const [carrier, setCarrier] = useState('')
  const [tracking, setTracking] = useState('')
  const [lines, setLines] = useState(value.lines.map(l => ({ line_id: l.id, warehouse_id: 0, batch_id: 0 })))
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const submit = async () => { setBusy(true); setError(''); try { await api(`/api/orders/${value.id}/ship`, { body: { carrier, tracking_no: tracking, lines } }); onDone() } catch (e) { setError(errText(e)) } finally { setBusy(false) } }
  return <Drawer title={`发货 · ${value.no}`} width={740} onClose={() => { if (!busy) onClose() }} foot={<><span className="help">整单发货，任一行库存不足都不会扣减</span><span className="grow" /><button className="btn btn-secondary" disabled={busy} onClick={onClose}>取消</button><button className="btn btn-primary" disabled={busy || lines.some(l => !l.warehouse_id)} onClick={submit}>{busy ? '发货中…' : '确认发货'}</button></>}>
    <div className="form-row"><Field label="快递公司"><input className="input" maxLength={100} value={carrier} onChange={e => setCarrier(e.target.value)} placeholder="如 顺丰" /></Field><Field label="快递单号"><input className="input mono" maxLength={100} value={tracking} onChange={e => setTracking(e.target.value)} /></Field></div>
    {value.lines.map((l, i) => <ShipLine key={l.id} item={l} index={i} warehouse={lines[i].warehouse_id} batch={lines[i].batch_id} warehouses={items || []} onChange={(w, b) => setLines(ls => ls.map((x, j) => i === j ? { ...x, warehouse_id: w, batch_id: b } : x))} />)}
    {error && <div className="form-err" role="alert">{error}</div>}
  </Drawer>
}

export function ReturnForm({ value, onClose, onDone }: { value: Order; onClose: () => void; onDone: () => void }) {
  const { items } = useWarehouses()
  const warehouses = (items || []).filter(w => w.type !== 'dropship')
  const [refund, setRefund] = useState('')
  const [reason, setReason] = useState('')
  const [rows, setRows] = useState(value.lines.map(l => ({ line_id: l.id, qty: '0', action: 'restock' as ReturnAction, warehouse_id: 0 })))
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const refunded = value.returns.reduce((s, r) => s + r.refund_amount, 0)
  const maxRefund = value.paid_amount - refunded
  const valid = Number(refund) >= 0 && toFen(refund) <= maxRefund && (toFen(refund) > 0 || rows.some(r => Number(r.qty) > 0)) && rows.every((r, i) => Number.isSafeInteger(Number(r.qty)) && Number(r.qty) >= 0 && Number(r.qty) <= value.lines[i].qty - value.lines[i].returned_qty && (!Number(r.qty) || r.action !== 'restock' || r.warehouse_id))
  const upd = (i: number, patch: Partial<typeof rows[number]>) => setRows(rs => rs.map((r, j) => i === j ? { ...r, ...patch } : r))
  const submit = async () => { setBusy(true); setError(''); try { await api(`/api/orders/${value.id}/returns`, { body: { refund_amount: toFen(refund), reason, lines: rows.filter(r => Number(r.qty) > 0).map(r => ({ ...r, qty: Number(r.qty), warehouse_id: r.action === 'restock' ? r.warehouse_id : 0 })) } }); onDone() } catch (e) { setError(errText(e)) } finally { setBusy(false) } }
  return <Drawer title={`登记售后 · ${value.no}`} width={780} onClose={() => { if (!busy) onClose() }} foot={<><button className="btn btn-secondary" disabled={busy} onClick={onClose}>取消</button><button className="btn btn-primary" disabled={busy || !valid} onClick={submit}>{busy ? '登记中…' : '确认登记售后'}</button></>}>
    <Field label={`本次退款（${value.currency}）`} hint={`已退 ${money(refunded, value.currency)}，最多还可退 ${money(maxRefund, value.currency)}`}><input className="input mono" type="number" min={0} max={maxRefund / 100} step="0.01" value={refund} onChange={e => setRefund(e.target.value)} /></Field>
    <div className="help">只退款时，各行数量留 0 即可。填写数量时会累计售后件数；回仓按原发货成本与等级入库，报损和不退货退款不增加库存。</div>
    {value.lines.map((l, i) => <section className="commerce-receipt" key={l.id}><div className="inline"><span className="mono">{l.code}</span><strong>{l.title}</strong><span className="grow" /><span className="t3 mono">剩余可处理 {l.qty - l.returned_qty}</span></div><div className="commerce-form-three"><Field label="本次数量"><input className="input mono" type="number" min={0} max={l.qty - l.returned_qty} step={1} value={rows[i].qty} onChange={e => upd(i, { qty: e.target.value })} /></Field><Field label="处理方式"><select className="input" value={rows[i].action} onChange={e => upd(i, { action: e.target.value as ReturnAction })}>{Object.entries(returnAction).map(([k, label]) => <option key={k} value={k}>{label}</option>)}</select></Field><Field label="回仓仓库"><select className="input" disabled={rows[i].action !== 'restock'} value={rows[i].warehouse_id} onChange={e => upd(i, { warehouse_id: Number(e.target.value) })}><option value={0}>选择仓库</option>{warehouses.map(w => <option key={w.id} value={w.id}>{w.name}</option>)}</select></Field></div></section>)}
    <Field label="售后原因"><textarea className="textarea" rows={3} maxLength={1000} value={reason} onChange={e => setReason(e.target.value)} /></Field>{error && <div className="form-err" role="alert">{error}</div>}
  </Drawer>
}
