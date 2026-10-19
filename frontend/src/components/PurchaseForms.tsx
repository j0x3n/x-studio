import { useState } from 'react'
import { Plus, X } from 'lucide-react'
import { api, type Page } from '../lib/api'
import { useToast, errText } from '../lib/toast'
import { fenToInput, toFen, type InvItem } from '../lib/inventory'
import { money, type Purchase, type Supplier } from '../lib/commerce'
import { Drawer, Field } from './ui'
import { ItemPicker, ProductPicker, useWarehouses } from './InventoryForms'
import { useRecord } from './CommerceShared'

type Line = { item: InvItem | null; item_id: number; qty: string; amount: string }
const fresh = (): Line => ({ item: null, item_id: 0, qty: '1', amount: '' })

export function PurchaseForm({ value, supplierID = 0, onClose, onDone }: { value?: Purchase; supplierID?: number; onClose: () => void; onDone: (id: number) => void }) {
  const toast = useToast()
  const { data: suppliers } = useRecord<Page<Supplier>>('/api/suppliers?page_size=200')
  const [supplier, setSupplier] = useState(value?.supplier_id || supplierID)
  const [currency, setCurrency] = useState(value?.currency || 'CNY')
  const [shipping, setShipping] = useState(fenToInput(value?.shipping_fee ?? 0))
  const [expected, setExpected] = useState(value?.expected_at.slice(0, 10) || '')
  const [sample, setSample] = useState(value?.is_sample || false)
  const [product, setProduct] = useState<{ id: number; name: string } | null>(value?.product_id ? { id: value.product_id, name: `商品 #${value.product_id}` } : null)
  const [note, setNote] = useState(value?.note || '')
  const [lines, setLines] = useState<Line[]>(value ? value.lines.map(l => ({ item: { id: l.item_id, code: l.code, name: l.name, unit: l.unit } as InvItem, item_id: l.item_id, qty: String(l.qty), amount: fenToInput(l.amount) })) : [fresh()])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const update = (i: number, patch: Partial<Line>) => setLines(ls => ls.map((l, j) => i === j ? { ...l, ...patch } : l))
  const total = lines.reduce((s, l) => s + toFen(l.amount), 0) + toFen(shipping)
  const valid = supplier > 0 && /^[A-Z]{3}$/.test(currency) && Number(shipping) >= 0 && lines.length > 0 && lines.every(l => l.item_id && Number.isSafeInteger(Number(l.qty)) && Number(l.qty) > 0 && l.amount.trim() !== '' && Number(l.amount) >= 0)
  const save = async () => {
    if (!valid) return
    setBusy(true); setError('')
    try {
      const r = await api<{ id: number }>('/api/purchase-orders' + (value ? `/${value.id}` : ''), { method: value ? 'PATCH' : 'POST', body: { supplier_id: supplier, currency, shipping_fee: toFen(shipping), expected_at: expected, is_sample: sample, product_id: product?.id || null, note, lines: lines.map(l => ({ item_id: l.item_id, qty: Number(l.qty), amount: toFen(l.amount) })) } })
      toast(value ? '采购单已保存' : '采购草稿已创建', 'ok'); onDone(r.id)
    } catch (e) { setError(errText(e)) } finally { setBusy(false) }
  }
  return <Drawer title={value ? `编辑 ${value.no}` : '新建采购单'} width={860} onClose={() => { if (!busy) onClose() }} foot={<><span className="t2 mono">合计 {money(total, /^[A-Z]{3}$/.test(currency) ? currency : 'CNY')}</span><span className="grow" /><button className="btn btn-secondary" disabled={busy} onClick={onClose}>取消</button><button className="btn btn-primary" disabled={busy || !valid} onClick={save}>{busy ? '保存中…' : '保存草稿'}</button></>}>
    <div className="form-row"><Field label="供应商" req><select className="input" value={supplier} onChange={e => setSupplier(Number(e.target.value))}><option value={0}>选择供应商</option>{value && !suppliers?.items.some(s => s.id === value.supplier_id) && <option value={value.supplier_id}>{value.supplier_name}（原供应商）</option>}{suppliers?.items.map(s => <option key={s.id} value={s.id}>{s.name}</option>)}</select></Field><Field label="预计到货"><input className="input mono" type="date" value={expected} onChange={e => setExpected(e.target.value)} /></Field></div>
    <div className="form-row"><Field label="币种"><select className="input" value={currency} onChange={e => setCurrency(e.target.value)}>{Array.from(new Set(['CNY', 'TWD', 'USD', currency])).map(c => <option key={c}>{c}</option>)}</select></Field><Field label="运费" hint="按各行货款比例分摊进库存成本"><input className="input mono" type="number" min={0} step="0.01" value={shipping} onChange={e => setShipping(e.target.value)} /></Field></div>
    <div className="help">金额填该行总货款，数量按物料的基本单位录入。保存为草稿后，可在详情中确认下单。</div>
    <div className="commerce-lines"><div className="commerce-line-head"><span>采购明细</span><span>数量</span><span>总货款</span><span /></div>{lines.map((l, i) => <div className="commerce-line" key={i}><div><ItemPicker value={l.item} onChange={it => update(i, { item: it, item_id: it?.id || 0 })} />{l.item && <span className="help">基本单位：{l.item.unit}</span>}</div><input className="input mono" type="number" min={1} step={1} aria-label={`第${i + 1}行数量`} value={l.qty} onChange={e => update(i, { qty: e.target.value })} /><input className="input mono" type="number" min={0} step="0.01" aria-label={`第${i + 1}行总货款`} value={l.amount} onChange={e => update(i, { amount: e.target.value })} /><button className="btn btn-ghost btn-icon" title="移除明细" disabled={lines.length === 1} onClick={() => setLines(ls => ls.filter((_, j) => i !== j))}><X /></button></div>)}</div>
    <button className="btn btn-secondary commerce-add" disabled={lines.length >= 200} onClick={() => setLines(ls => [...ls, fresh()])}><Plus />添加物料</button>
    <label className="inline"><input className="check" type="checkbox" checked={sample} onChange={e => setSample(e.target.checked)} />拍样单</label>
    {sample && <Field label="关联商品（选填）"><ProductPicker value={product} onChange={setProduct} /></Field>}
    <Field label="备注"><textarea className="textarea" rows={3} maxLength={1000} value={note} onChange={e => setNote(e.target.value)} /></Field>{error && <div className="form-err" role="alert">{error}</div>}
  </Drawer>
}

type ReceiptRow = { qty: string; rejected: string; grades: { grade: string; qty: string }[] }
export function ReceiptForm({ value, onClose, onDone }: { value: Purchase; onClose: () => void; onDone: () => void }) {
  const { items } = useWarehouses()
  const warehouses = (items || []).filter(w => w.type !== 'dropship')
  const [warehouse, setWarehouse] = useState(0)
  const wh = warehouse || warehouses[0]?.id || 0
  const [date, setDate] = useState(new Date().toLocaleDateString('sv-SE'))
  const [note, setNote] = useState('')
  const [rows, setRows] = useState<ReceiptRow[]>(value.lines.map(l => ({ qty: String(Math.max(0, l.qty - l.received_qty)), rejected: '0', grades: [] })))
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const update = (i: number, patch: Partial<ReceiptRow>) => setRows(rs => rs.map((r, j) => i === j ? { ...r, ...patch } : r))
  const valid = wh && rows.some(r => Number(r.qty) > 0) && rows.every(r => {
    const qty = Number(r.qty), rejected = Number(r.rejected)
    if (!Number.isSafeInteger(qty) || qty < 0 || !Number.isSafeInteger(rejected) || rejected < 0 || rejected > qty) return false
    return !r.grades.length || r.grades.every(g => Number.isSafeInteger(Number(g.qty)) && Number(g.qty) > 0) && r.grades.reduce((s, g) => s + Number(g.qty), 0) === qty - rejected
  })
  const submit = async () => {
    if (!valid) return
    setBusy(true); setError('')
    try { await api(`/api/purchase-orders/${value.id}/receipts`, { body: { warehouse_id: wh, received_at: date, note, lines: rows.flatMap((r, i) => Number(r.qty) > 0 ? [{ line_id: value.lines[i].id, qty: Number(r.qty), rejected: Number(r.rejected), grades: r.grades.map(g => ({ grade: g.grade, qty: Number(g.qty) })) }] : []) } }); onDone() }
    catch (e) { setError(errText(e)) } finally { setBusy(false) }
  }
  return <Drawer title={`到货质检 · ${value.no}`} width={780} onClose={() => { if (!busy) onClose() }} foot={<><span className="help">合格品入库，不合格品生成待退单</span><span className="grow" /><button className="btn btn-secondary" disabled={busy} onClick={onClose}>取消</button><button className="btn btn-primary" disabled={busy || !valid} onClick={submit}>{busy ? '登记中…' : '确认质检入库'}</button></>}>
    <div className="form-row"><Field label="入库仓库" req><select className="input" value={wh} onChange={e => setWarehouse(Number(e.target.value))}><option value={0}>选择仓库</option>{warehouses.map(w => <option key={w.id} value={w.id}>{w.name}</option>)}</select></Field><Field label="到货日期"><input className="input mono" type="date" value={date} onChange={e => setDate(e.target.value)} /></Field></div>
    <div className="help">本次没有到货的行填 0。实收包含不合格数量；未分等级时，合格品按无等级入库。</div>
    {value.lines.map((l, i) => { const r = rows[i]; return <section className="commerce-receipt" key={l.id}><div className="inline"><span className="mono">{l.code}</span><strong>{l.name}</strong><span className="grow" /><span className="t3 mono">已收 {l.received_qty} / {l.qty} {l.unit}</span></div><div className="form-row"><Field label={`本次实收（${l.unit}）`}><input className="input mono" type="number" min={0} step={1} value={r.qty} onChange={e => update(i, { qty: e.target.value })} /></Field><Field label="其中不合格"><input className="input mono" type="number" min={0} max={Number(r.qty)} step={1} value={r.rejected} onChange={e => update(i, { rejected: e.target.value })} /></Field></div>
      {r.grades.map((g, j) => <div className="commerce-grade" key={j}><input className="input" placeholder="等级，如 AA" maxLength={20} value={g.grade} onChange={e => update(i, { grades: r.grades.map((x, k) => j === k ? { ...x, grade: e.target.value } : x) })} /><input className="input mono" aria-label={`第${i + 1}行等级${j + 1}数量`} type="number" min={1} step={1} value={g.qty} onChange={e => update(i, { grades: r.grades.map((x, k) => j === k ? { ...x, qty: e.target.value } : x) })} /><button className="btn btn-ghost btn-icon" title="移除等级" onClick={() => update(i, { grades: r.grades.filter((_, k) => j !== k) })}><X /></button></div>)}
      <div className="inline"><button className="btn btn-ghost btn-sm" disabled={Number(r.qty) - Number(r.rejected) <= 0} onClick={() => update(i, { grades: [...r.grades, { grade: '', qty: String(Math.max(0, Number(r.qty) - Number(r.rejected) - r.grades.reduce((s, g) => s + Number(g.qty), 0))) }] })}><Plus />按等级拆分</button>{r.grades.length > 0 && <span className="help">等级合计 {r.grades.reduce((s, g) => s + Number(g.qty), 0)} / 合格 {Number(r.qty) - Number(r.rejected)}</span>}</div>
    </section> })}
    <Field label="质检备注"><textarea className="textarea" maxLength={1000} rows={2} value={note} onChange={e => setNote(e.target.value)} /></Field>{error && <div className="form-err" role="alert">{error}</div>}
  </Drawer>
}
