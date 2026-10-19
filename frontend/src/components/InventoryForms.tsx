import { useEffect, useMemo, useState } from 'react'
import { Plus, X, Search } from 'lucide-react'
import { api, qs, type Page } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { useCategories } from '../lib/categories'
import { kindName, whTypeName, specText, toFen, fenToInput, yuan, type InvItem, type Warehouse, type Batch, type ItemKind, type WhType, type Unit, type Dropship } from '../lib/inventory'
import { Drawer, Field } from './ui'

export const useCostPerm = () => { const { can } = useAuth(); return can('product.cost') || can('finance.view') }

export function useWarehouses(all = false) {
  const [list, setList] = useState<Warehouse[] | null>(null)
  const [stamp, setStamp] = useState(0)
  useEffect(() => {
    api<Warehouse[] | { items: Warehouse[] }>('/api/inventory/warehouses').then(r => setList(Array.isArray(r) ? r : r.items || [])).catch(() => setList([]))
  }, [stamp])
  const items = list ? list.filter(w => all || !w.archived).sort((a, b) => a.sort - b.sort || a.id - b.id) : null
  return { items, reload: () => setStamp(s => s + 1) }
}

// 搜物料：输入编码或名称，下拉选一个
export function ItemPicker({ value, onChange, kind, autoFocus }: { value: InvItem | null; onChange: (it: InvItem | null) => void; kind?: ItemKind; autoFocus?: boolean }) {
  const [q, setQ] = useState('')
  const [open, setOpen] = useState(false)
  const [list, setList] = useState<InvItem[]>([])
  useEffect(() => {
    if (!open) return
    const t = setTimeout(() => api<Page<InvItem>>('/api/inventory/items' + qs({ q, kind, page_size: 20 })).then(r => setList(r.items || [])).catch(() => setList([])), 180)
    return () => clearTimeout(t)
  }, [q, kind, open])
  if (value) return (
    <div className="inv-picked input"><span className="mono">{value.code}</span><span className="ellipsis grow">{value.name}</span><button className="tag-x" onClick={() => onChange(null)}><X /></button></div>
  )
  return (
    <div className="inv-picker">
      <label className="field"><Search /><input autoFocus={autoFocus} placeholder="编码或名称" value={q} onChange={e => { setQ(e.target.value); setOpen(true) }} onFocus={() => setOpen(true)} onBlur={() => setTimeout(() => setOpen(false), 150)} /></label>
      {open && list.length > 0 && (
        <div className="menu inv-picker-menu">{list.map(it => (
          <div key={it.id} className="menu-item" onMouseDown={() => { onChange(it); setQ(''); setOpen(false) }}>
            <span className="mono">{it.code}</span><span className="ellipsis grow">{it.name}{it.spec && Object.keys(it.spec).length ? <span className="t3"> · {specText(it.spec)}</span> : null}</span><span className="t3 mono">{it.on_hand}</span>
          </div>
        ))}</div>
      )}
    </div>
  )
}

// 数量输入：可以按换算单位录，提交时换成基本单位
function QtyInput({ item, value, onChange }: { item: InvItem | null; value: number; onChange: (n: number) => void }) {
  const units: Unit[] = [{ name: item?.unit || '', factor: 1 }, ...(item?.units || [])]
  const [unit, setUnit] = useState(1)
  const [raw, setRaw] = useState(value ? String(value) : '')
  useEffect(() => { setUnit(1); setRaw(value ? String(value) : '') }, [item?.id]) // eslint-disable-line react-hooks/exhaustive-deps
  const set = (r: string, f: number) => { setRaw(r); setUnit(f); onChange(Math.round((Number(r) || 0) * f)) }
  return (
    <div className="inv-qty">
      <input className="input mono" inputMode="numeric" placeholder="数量" value={raw} onChange={e => set(e.target.value.replace(/[^\d.]/g, ''), unit)} />
      {units.length > 1 ? (
        <select className="input" value={unit} onChange={e => set(raw, Number(e.target.value))}>{units.map(u => <option key={u.factor} value={u.factor}>{u.name}</option>)}</select>
      ) : <span className="t3 inv-unit">{item?.unit}</span>}
    </div>
  )
}

function BatchSelect({ item, warehouse, value, onChange }: { item: InvItem | null; warehouse: number; value: number; onChange: (id: number) => void }) {
  const [list, setList] = useState<Batch[]>([])
  useEffect(() => {
    if (!item || !warehouse) { setList([]); return }
    api<Batch[] | { items: Batch[] }>('/api/inventory/batches' + qs({ item_id: item.id, warehouse_id: warehouse })).then(r => setList(Array.isArray(r) ? r : r.items || [])).catch(() => setList([]))
  }, [item?.id, warehouse]) // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <select className="input" value={value} onChange={e => onChange(Number(e.target.value))}>
      <option value={0}>先进先出</option>
      {list.map(b => <option key={b.id} value={b.id}>#{b.id} {b.grade || '无等级'} · 剩 {b.qty_left} · {b.received_at.slice(0, 10)}</option>)}
    </select>
  )
}

type MoveKind = 'other_in' | 'other_out' | 'scrap'
type Line = { key: number; item: InvItem | null; qty: number; cost: string; grade: string; batch: number }
let lineSeq = 0
const newLine = (item: InvItem | null = null): Line => ({ key: ++lineSeq, item, qty: 0, cost: '', grade: '', batch: 0 })

// 手工入库 / 出库 / 报损，一次多行
export function MoveDrawer({ kind: kind0 = 'other_in', item, onClose, onDone }: { kind?: MoveKind; item?: InvItem | null; onClose: () => void; onDone: () => void }) {
  const toast = useToast()
  const costPerm = useCostPerm()
  const { items: whs } = useWarehouses()
  const stockWhs = (whs || []).filter(w => w.type !== 'dropship')
  const [kind, setKind] = useState<MoveKind>(kind0)
  const [wh, setWh] = useState(0)
  const [date, setDate] = useState(new Date().toISOString().slice(0, 10))
  const [note, setNote] = useState('')
  const [lines, setLines] = useState<Line[]>([newLine(item || null)])
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (!wh && stockWhs[0]) setWh(stockWhs[0].id) }, [stockWhs, wh])
  const isIn = kind === 'other_in'
  const upd = (k: number, p: Partial<Line>) => setLines(ls => ls.map(l => (l.key === k ? { ...l, ...p } : l)))
  const ok = wh && lines.some(l => l.item && l.qty > 0)
  const submit = async () => {
    setBusy(true)
    try {
      const body = {
        kind, warehouse_id: wh, note,
        lines: lines.filter(l => l.item && l.qty > 0).map(l => isIn
          ? { item_id: l.item!.id, qty: l.qty, cost: costPerm ? toFen(l.cost) : 0, grade: l.grade.trim(), received_at: date }
          : { item_id: l.item!.id, qty: l.qty, batch_id: l.batch })
      }
      await api('/api/inventory/moves', { body })
      toast(isIn ? '已入库' : kind === 'scrap' ? '已报损' : '已出库', 'ok')
      onDone()
    } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  return (
    <Drawer title="出入库" width={760} onClose={onClose}
      foot={<><span className="t3 fs-12">整单一起提交，任何一行不够都不会执行</span><span className="grow" /><button className="btn btn-secondary" onClick={onClose}>取消</button><button className="btn btn-primary" disabled={busy || !ok} onClick={submit}>{isIn ? '确认入库' : kind === 'scrap' ? '确认报损' : '确认出库'}</button></>}>
      <div className="inv-form-row">
        <div className="segmented">{([['other_in', '手工入库'], ['other_out', '手工出库'], ['scrap', '报损']] as [MoveKind, string][]).map(([k, l]) => <button key={k} className={kind === k ? 'active' : ''} onClick={() => setKind(k)}>{l}</button>)}</div>
        <select className="input" style={{ width: 160 }} value={wh} onChange={e => setWh(Number(e.target.value))}>{stockWhs.map(w => <option key={w.id} value={w.id}>{w.name}（{whTypeName[w.type]}）</option>)}</select>
        {isIn && <input className="input" type="date" style={{ width: 150 }} value={date} onChange={e => setDate(e.target.value)} title="到货日期，先进先出按它排" />}
      </div>
      {isIn && <div className="t3 fs-12">期初库存、零散进货、样品用「手工入库」；向供应商下单的走采购单。{costPerm ? '成本填这一行的总金额。' : '你没有看成本的权限，成本记 0。'}</div>}
      <table className="table inv-lines">
        <thead><tr><th style={{ width: '36%' }}>物料</th><th>数量</th>{isIn ? <>{costPerm && <th>总成本（元）</th>}<th>等级</th></> : <th>批次</th>}<th /></tr></thead>
        <tbody>{lines.map((l, i) => (
          <tr key={l.key}>
            <td><ItemPicker value={l.item} onChange={it => upd(l.key, { item: it, batch: 0 })} autoFocus={i === lines.length - 1 && !l.item} /></td>
            <td><QtyInput item={l.item} value={l.qty} onChange={n => upd(l.key, { qty: n })} />{!isIn && l.item && <div className="t4 fs-12">现有 {l.item.on_hand}，可售 {l.item.unlimited ? '不限' : l.item.available}</div>}</td>
            {isIn ? <>
              {costPerm && <td><input className="input mono" inputMode="decimal" placeholder="0.00" value={l.cost} onChange={e => upd(l.key, { cost: e.target.value })} />{l.qty > 0 && toFen(l.cost) > 0 && <div className="t4 fs-12">单价 {yuan(Math.round(toFen(l.cost) / l.qty))}</div>}</td>}
              <td><input className="input" maxLength={20} placeholder="如 AA" value={l.grade} onChange={e => upd(l.key, { grade: e.target.value })} /></td>
            </> : <td><BatchSelect item={l.item} warehouse={wh} value={l.batch} onChange={b => upd(l.key, { batch: b })} /></td>}
            <td>{lines.length > 1 && <button className="btn btn-ghost btn-icon btn-sm" onClick={() => setLines(ls => ls.filter(x => x.key !== l.key))}><X /></button>}</td>
          </tr>
        ))}</tbody>
      </table>
      <div><button className="btn btn-ghost btn-sm" onClick={() => setLines(ls => [...ls, newLine()])}><Plus />加一行</button></div>
      <Field label="备注"><input className="input" maxLength={200} value={note} onChange={e => setNote(e.target.value)} placeholder={isIn ? '如 期初库存' : '如 拍摄用掉'} /></Field>
    </Drawer>
  )
}

// 调拨：从一个仓搬到另一个仓
export function TransferDrawer({ item, onClose, onDone }: { item?: InvItem | null; onClose: () => void; onDone: () => void }) {
  const toast = useToast()
  const { items: whs } = useWarehouses()
  const stockWhs = (whs || []).filter(w => w.type !== 'dropship')
  const [from, setFrom] = useState(0)
  const [to, setTo] = useState(0)
  const [note, setNote] = useState('')
  const [lines, setLines] = useState<Line[]>([newLine(item || null)])
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (!from && stockWhs[0]) { setFrom(stockWhs[0].id); setTo(stockWhs[1]?.id || 0) } }, [stockWhs, from])
  const upd = (k: number, p: Partial<Line>) => setLines(ls => ls.map(l => (l.key === k ? { ...l, ...p } : l)))
  const submit = async () => {
    setBusy(true)
    try {
      await api('/api/inventory/transfers', { body: { from, to, note, lines: lines.filter(l => l.item && l.qty > 0).map(l => ({ item_id: l.item!.id, qty: l.qty, batch_id: l.batch })) } })
      toast('已调拨', 'ok'); onDone()
    } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  return (
    <Drawer title="调拨" width={680} onClose={onClose}
      foot={<><span className="grow" /><button className="btn btn-secondary" onClick={onClose}>取消</button><button className="btn btn-primary" disabled={busy || !from || !to || from === to || !lines.some(l => l.item && l.qty > 0)} onClick={submit}>确认调拨</button></>}>
      {stockWhs.length < 2 ? <div className="t3">至少要有两个自有仓或档口才能调拨，先在「仓库」里加一个。</div> : <>
        <div className="inv-form-row">
          <select className="input" style={{ width: 180 }} value={from} onChange={e => setFrom(Number(e.target.value))}>{stockWhs.map(w => <option key={w.id} value={w.id}>{w.name}</option>)}</select>
          <span className="t3">调到</span>
          <select className="input" style={{ width: 180 }} value={to} onChange={e => setTo(Number(e.target.value))}><option value={0}>选择目标仓</option>{stockWhs.filter(w => w.id !== from).map(w => <option key={w.id} value={w.id}>{w.name}</option>)}</select>
        </div>
        <table className="table inv-lines">
          <thead><tr><th style={{ width: '42%' }}>物料</th><th>数量</th><th>批次</th><th /></tr></thead>
          <tbody>{lines.map(l => (
            <tr key={l.key}>
              <td><ItemPicker value={l.item} onChange={it => upd(l.key, { item: it, batch: 0 })} /></td>
              <td><QtyInput item={l.item} value={l.qty} onChange={n => upd(l.key, { qty: n })} /></td>
              <td><BatchSelect item={l.item} warehouse={from} value={l.batch} onChange={b => upd(l.key, { batch: b })} /></td>
              <td>{lines.length > 1 && <button className="btn btn-ghost btn-icon btn-sm" onClick={() => setLines(ls => ls.filter(x => x.key !== l.key))}><X /></button>}</td>
            </tr>
          ))}</tbody>
        </table>
        <div><button className="btn btn-ghost btn-sm" onClick={() => setLines(ls => [...ls, newLine()])}><Plus />加一行</button></div>
        <Field label="备注"><input className="input" maxLength={200} value={note} onChange={e => setNote(e.target.value)} /></Field>
      </>}
    </Drawer>
  )
}

// 新建 / 修改物料
export function ItemDrawer({ item, product, onClose, onDone }: { item?: InvItem | null; product?: { id: number; name: string; category_id?: number | null } | null; onClose: () => void; onDone: (it: InvItem) => void }) {
  const toast = useToast()
  const { items: cats } = useCategories()
  const [code, setCode] = useState(item?.code || '')
  const [name, setName] = useState(item?.name || product?.name || '')
  const [kind, setKind] = useState<ItemKind>(item?.kind || (product ? 'sku' : 'sku'))
  const [prod, setProd] = useState<{ id: number; name: string } | null>(item?.product || product || null)
  const [cat, setCat] = useState(String(item?.category_id ?? product?.category_id ?? ''))
  const [spec, setSpec] = useState<[string, string][]>(Object.entries(item?.spec || {}))
  const [unit, setUnit] = useState(item?.unit || '个')
  const [units, setUnits] = useState<Unit[]>(item?.units || [])
  const [safety, setSafety] = useState(String(item?.safety_stock ?? 0))
  const [note, setNote] = useState(item?.note || '')
  const [archived, setArchived] = useState(!!item?.archived)
  const [busy, setBusy] = useState(false)
  const submit = async () => {
    setBusy(true)
    try {
      const body = {
        code: code.trim(), name: name.trim(), kind, product_id: kind === 'sku' ? prod?.id ?? null : null, category_id: cat ? Number(cat) : null,
        spec: Object.fromEntries(spec.filter(([k, v]) => k.trim() && v.trim()).map(([k, v]) => [k.trim(), v.trim()])),
        unit: unit.trim(), units: units.filter(u => u.name.trim() && u.factor > 1), safety_stock: Number(safety) || 0, note, ...(item ? { archived } : {})
      }
      const r = item ? await api<{ item?: InvItem } & InvItem>(`/api/inventory/items/${item.id}`, { method: 'PATCH', body }) : await api<{ item?: InvItem } & InvItem>('/api/inventory/items', { body })
      toast(item ? '已保存' : '已新建', 'ok')
      onDone(r.item || r)
    } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  return (
    <Drawer title={item ? '修改物料' : '新建物料'} width={520} onClose={onClose}
      foot={<><span className="grow" /><button className="btn btn-secondary" onClick={onClose}>取消</button><button className="btn btn-primary" disabled={busy || !code.trim() || !name.trim() || !unit.trim()} onClick={submit}>保存</button></>}>
      <div className="segmented">{(Object.keys(kindName) as ItemKind[]).map(k => <button key={k} className={kind === k ? 'active' : ''} onClick={() => setKind(k)}>{kindName[k]}</button>)}</div>
      <div className="form-row">
        <Field label="编码" req hint={kind === 'sku' ? '就是平台上的商家编码，录单和导入靠它匹配' : '唯一，建议带品类前缀'}><input className="input mono" maxLength={64} value={code} onChange={e => setCode(e.target.value)} /></Field>
        <Field label="名称" req><input className="input" maxLength={100} value={name} onChange={e => setName(e.target.value)} /></Field>
      </div>
      {kind === 'sku' && <Field label="挂在哪个商品下" hint="一个商品的每个规格建一个成品物料"><ProductPicker value={prod} onChange={setProd} /></Field>}
      <div className="form-row">
        <Field label="品类"><select className="input" value={cat} onChange={e => setCat(e.target.value)}><option value="">不限</option>{(cats || []).map(c => <option key={c.id} value={c.id}>{c.name}</option>)}</select></Field>
        <Field label="安全库存" hint="可售数低于它时提醒补货，0 = 不提醒"><input className="input mono" inputMode="numeric" value={safety} onChange={e => setSafety(e.target.value.replace(/\D/g, ''))} /></Field>
      </div>
      <Field label="规格" hint="如 尺寸 8-9mm、颜色 白，最多 10 项">
        <div className="inv-kv">
          {spec.map(([k, v], i) => (
            <div key={i} className="inv-kv-row">
              <input className="input" placeholder="名称" value={k} onChange={e => setSpec(s => s.map((x, j) => (j === i ? [e.target.value, x[1]] : x)))} />
              <input className="input" placeholder="值" value={v} onChange={e => setSpec(s => s.map((x, j) => (j === i ? [x[0], e.target.value] : x)))} />
              <button className="btn btn-ghost btn-icon btn-sm" onClick={() => setSpec(s => s.filter((_, j) => j !== i))}><X /></button>
            </div>
          ))}
          {spec.length < 10 && <button className="btn btn-ghost btn-sm" onClick={() => setSpec(s => [...s, ['', '']])}><Plus />加规格</button>}
        </div>
      </Field>
      <div className="form-row">
        <Field label="基本单位" req hint="库存都按它记，有出入库后不能改"><input className="input" maxLength={8} value={unit} onChange={e => setUnit(e.target.value)} placeholder="颗、对、条、个、克" /></Field>
        <Field label="换算单位" hint="如 1 串 = 40 颗，只用于录入和显示">
          <div className="inv-kv">
            {units.map((u, i) => (
              <div key={i} className="inv-kv-row">
                <span className="t3">1</span><input className="input" style={{ width: 64 }} value={u.name} onChange={e => setUnits(s => s.map((x, j) => (j === i ? { ...x, name: e.target.value } : x)))} />
                <span className="t3">=</span><input className="input mono" style={{ width: 72 }} inputMode="numeric" value={u.factor || ''} onChange={e => setUnits(s => s.map((x, j) => (j === i ? { ...x, factor: Number(e.target.value.replace(/\D/g, '')) } : x)))} />
                <span className="t3">{unit}</span>
                <button className="btn btn-ghost btn-icon btn-sm" onClick={() => setUnits(s => s.filter((_, j) => j !== i))}><X /></button>
              </div>
            ))}
            {units.length < 5 && <button className="btn btn-ghost btn-sm" onClick={() => setUnits(s => [...s, { name: '', factor: 0 }])}><Plus />加换算</button>}
          </div>
        </Field>
      </div>
      <Field label="备注"><textarea className="textarea" maxLength={500} value={note} onChange={e => setNote(e.target.value)} /></Field>
      {item && <label className="inline"><input type="checkbox" className="check" checked={archived} onChange={e => setArchived(e.target.checked)} />归档（不再入库，列表默认不显示；有库存或占用时不能归档）</label>}
    </Drawer>
  )
}

export function ProductPicker({ value, onChange }: { value: { id: number; name: string } | null; onChange: (p: { id: number; name: string } | null) => void }) {
  const [q, setQ] = useState('')
  const [list, setList] = useState<{ id: number; name: string }[]>([])
  const [open, setOpen] = useState(false)
  useEffect(() => {
    if (!open) return
    const t = setTimeout(() => api<Page<{ id: number; name: string }>>('/api/products' + qs({ q, page_size: 20 })).then(r => setList(r.items || [])).catch(() => setList([])), 180)
    return () => clearTimeout(t)
  }, [q, open])
  if (value) return <div className="inv-picked input"><span className="ellipsis grow">{value.name || '无权查看'}</span><button className="tag-x" onClick={() => onChange(null)}><X /></button></div>
  return (
    <div className="inv-picker">
      <label className="field"><Search /><input placeholder="搜商品名，可不挂" value={q} onChange={e => { setQ(e.target.value); setOpen(true) }} onFocus={() => setOpen(true)} onBlur={() => setTimeout(() => setOpen(false), 150)} /></label>
      {open && list.length > 0 && <div className="menu inv-picker-menu">{list.map(p => <div key={p.id} className="menu-item" onMouseDown={() => { onChange(p); setOpen(false) }}><span className="mono t3">#{p.id}</span><span className="ellipsis">{p.name}</span></div>)}</div>}
    </div>
  )
}

export function WarehouseDrawer({ wh, onClose, onDone }: { wh?: Warehouse | null; onClose: () => void; onDone: () => void }) {
  const toast = useToast()
  const [name, setName] = useState(wh?.name || '')
  const [type, setType] = useState<WhType>(wh?.type || 'own')
  const [address, setAddress] = useState(wh?.address || '')
  const [note, setNote] = useState(wh?.note || '')
  const [sort, setSort] = useState(String(wh?.sort ?? 0))
  const [archived, setArchived] = useState(!!wh?.archived)
  const [busy, setBusy] = useState(false)
  const submit = async () => {
    setBusy(true)
    try {
      const body = { name: name.trim(), address, note, sort: Number(sort) || 0, ...(wh ? { archived } : { type }) }
      if (wh) await api(`/api/inventory/warehouses/${wh.id}`, { method: 'PATCH', body }); else await api('/api/inventory/warehouses', { body })
      onDone()
    } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  return (
    <Drawer title={wh ? '修改仓库' : '新建仓库'} width={420} onClose={onClose}
      foot={<><span className="grow" /><button className="btn btn-secondary" onClick={onClose}>取消</button><button className="btn btn-primary" disabled={busy || !name.trim()} onClick={submit}>保存</button></>}>
      <Field label="类型" hint={wh ? '建好后不能改' : '代发 = 供应商直接发货，不算自有库存'}>
        <div className="segmented">{(Object.keys(whTypeName) as WhType[]).map(k => <button key={k} disabled={!!wh} className={type === k ? 'active' : ''} onClick={() => setType(k)}>{whTypeName[k]}</button>)}</div>
      </Field>
      <Field label="名称" req><input className="input" maxLength={50} value={name} onChange={e => setName(e.target.value)} /></Field>
      <Field label="地址"><input className="input" value={address} onChange={e => setAddress(e.target.value)} /></Field>
      <Field label="排序" hint="数字小的在前"><input className="input mono" type="number" value={sort} onChange={e => setSort(e.target.value)} /></Field>
      <Field label="备注"><textarea className="textarea" value={note} onChange={e => setNote(e.target.value)} /></Field>
      {wh && <label className="inline"><input type="checkbox" className="check" checked={archived} onChange={e => setArchived(e.target.checked)} />归档（有库存时不能归档）</label>}
    </Drawer>
  )
}

// 代发：某个代发仓供不供这个物料、可售多少、单价多少
export function DropshipDrawer({ item, row, onClose, onDone }: { item: InvItem; row?: Dropship | null; onClose: () => void; onDone: () => void }) {
  const toast = useToast()
  const costPerm = useCostPerm()
  const { items: whs } = useWarehouses()
  const drop = useMemo(() => (whs || []).filter(w => w.type === 'dropship'), [whs])
  const [wh, setWh] = useState(row?.warehouse_id || 0)
  const [unlimited, setUnlimited] = useState(row ? row.qty === null : true)
  const [qty, setQty] = useState(row?.qty != null ? String(row.qty) : '')
  const [cost, setCost] = useState(fenToInput(row?.unit_cost))
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (!wh && drop[0]) setWh(drop[0].id) }, [drop, wh])
  const submit = async () => {
    setBusy(true)
    try {
      await api(`/api/inventory/dropship/${wh}/${item.id}`, { method: 'PUT', body: { qty: unlimited ? null : Number(qty) || 0, ...(costPerm ? { unit_cost: cost ? toFen(cost) : null } : {}), currency: 'CNY' } })
      onDone()
    } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const remove = async () => {
    if (!row || !confirm('这个代发仓不再供这个物料？')) return
    try { await api(`/api/inventory/dropship/${row.warehouse_id}/${item.id}`, { method: 'DELETE' }); onDone() } catch (e) { toast(errText(e), 'error') }
  }
  return (
    <Drawer title={`代发 · ${item.code}`} width={420} onClose={onClose}
      foot={<>{row && <button className="btn btn-ghost" onClick={remove}>不再代发</button>}<span className="grow" /><button className="btn btn-secondary" onClick={onClose}>取消</button><button className="btn btn-primary" disabled={busy || !wh} onClick={submit}>保存</button></>}>
      {drop.length === 0 ? <div className="t3">还没有代发仓，先在「库存 → 仓库」里新建一个类型为「代发」的仓库。</div> : <>
        <Field label="代发仓"><select className="input" value={wh} disabled={!!row} onChange={e => setWh(Number(e.target.value))}>{drop.map(w => <option key={w.id} value={w.id}>{w.name}</option>)}</select></Field>
        <Field label="可售数" hint="供应商报来新数时直接改；不限时从这里发货不扣数">
          <div className="inv-form-row">
            <label className="inline"><input type="checkbox" className="check" checked={unlimited} onChange={e => setUnlimited(e.target.checked)} />不限</label>
            {!unlimited && <input className="input mono" style={{ width: 120 }} inputMode="numeric" value={qty} onChange={e => setQty(e.target.value.replace(/\D/g, ''))} />}
            <span className="t3">{item.unit}</span>
          </div>
        </Field>
        {costPerm && <Field label="代发单价（元）" hint="发货时按它记成本"><input className="input mono" inputMode="decimal" value={cost} onChange={e => setCost(e.target.value)} /></Field>}
      </>}
    </Drawer>
  )
}
