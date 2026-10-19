// 库存：类型、中文名、数量和金额的显示
export type ItemKind = 'sku' | 'material' | 'packaging'
export type WhType = 'own' | 'stall' | 'dropship'
export type Unit = { name: string; factor: number }
export type InvItem = {
  id: number; code: string; name: string; kind: ItemKind
  product: { id: number; name: string } | null; category_id: number | null
  spec: Record<string, string>; unit: string; units: Unit[]
  safety_stock: number; low: boolean; note?: string
  on_hand: number; dropship: number; unlimited: boolean; reserved: number; available: number
  cost_value?: number; archived: boolean; updated_at: string
}
export type Warehouse = { id: number; name: string; type: WhType; supplier_id: number | null; address: string; note: string; sort: number; archived: boolean; item_count?: number; cost_value?: number }
export type Batch = { id: number; item_id: number; warehouse_id: number; warehouse_name?: string; qty_in: number; qty_left: number; cost_in?: number; cost_left?: number; currency: string; grade: string; supplier_id: number | null; received_at: string; ref_type: string; ref_id: number; ref_label?: string; ref_path?: string }
export type Level = { warehouse_id: number; warehouse_name?: string; name?: string; type?: WhType; on_hand: number; qty?: number }
export type Dropship = { warehouse_id: number; warehouse_name?: string; item_id: number; qty: number | null; unit_cost?: number | null; currency: string; updated_at: string }
export type Reservation = { ref_type: string; ref_id: number; qty: number; ref_label?: string; ref_path?: string; created_at: string }
export type LedgerKind = 'purchase_in' | 'sale_out' | 'return_in' | 'transfer_out' | 'transfer_in' | 'count_gain' | 'count_loss' | 'other_in' | 'other_out' | 'scrap' | 'assemble_in' | 'assemble_out'
export type Ledger = {
  id: number; item_id: number; code?: string; item_name?: string; name?: string; warehouse_id: number; warehouse_name?: string
  batch_id: number | null; kind: LedgerKind; qty: number; cost?: number; currency: string
  ref_type: string; ref_id: number; ref_label?: string; ref_path?: string; note: string; user_name?: string; created_at: string
}
export type ItemDetail = InvItem & { levels: Level[]; dropship_rows?: Dropship[]; batches: Batch[]; reservations: Reservation[]; ledger: Ledger[] }
export type Count = { id: number; warehouse_id: number; warehouse_name?: string; status: 'draft' | 'done' | 'canceled'; note: string; created_by?: number; created_by_name?: string; created_at: string; done_at: string; line_count?: number; counted_count?: number }
export type CountLine = { item_id: number; code: string; name: string; unit?: string; system_qty: number; current_qty: number; counted_qty: number | null; unit_cost?: number | null; difference?: number | null }

export const kindName: Record<ItemKind, string> = { sku: '成品', material: '原料配件', packaging: '包装耗材' }
export const whTypeName: Record<WhType, string> = { own: '自有仓', stall: '档口', dropship: '代发' }
export const ledgerName: Record<LedgerKind, string> = {
  purchase_in: '采购入库', sale_out: '销售出库', return_in: '退货入库', transfer_out: '调出', transfer_in: '调入',
  count_gain: '盘盈', count_loss: '盘亏', other_in: '手工入库', other_out: '手工出库', scrap: '报损', assemble_in: '组装产出', assemble_out: '组装消耗'
}
export const refName: Record<string, string> = { manual: '手工', transfer: '调拨', count: '盘点', purchase_receipt: '采购到货', order: '订单', order_return: '售后退货' }
export const countStatus: Record<Count['status'], { label: string; pill: string }> = {
  draft: { label: '盘点中', pill: 'pill-warning' }, done: { label: '已完成', pill: 'pill-success' }, canceled: { label: '已取消', pill: 'pill-neutral' }
}

// 金额：分 → ¥1,234.56；没有成本权限时字段不存在，显示 —
export const yuan = (fen?: number | null) => (typeof fen === 'number' ? '¥' + (fen / 100).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 }) : '—')
// 输入框里的元 → 分（整数）
export const toFen = (s: string) => { const n = Number(s); return Number.isFinite(n) ? Math.round(n * 100) : 0 }
export const fenToInput = (fen?: number | null) => (typeof fen === 'number' ? (fen / 100).toFixed(2) : '')

// 数量带单位；有换算单位时补一句「= 2 串 + 5 颗」
export function qtyText(n: number, it?: { unit: string; units?: Unit[] }) {
  const u = it?.unit || ''
  const s = `${n.toLocaleString('zh-CN')}${u ? ' ' + u : ''}`
  const big = (it?.units || []).filter(x => x.factor > 1).sort((a, b) => b.factor - a.factor)[0]
  if (!big || Math.abs(n) < big.factor) return s
  const whole = Math.trunc(n / big.factor), rest = Math.abs(n % big.factor)
  return `${s}（${whole} ${big.name}${rest ? ` + ${rest} ${u}` : ''}）`
}
export const specText = (spec?: Record<string, string>) => Object.entries(spec || {}).map(([k, v]) => `${k} ${v}`).join(' · ')

// 可售数的显示：代发不限时写「不限」
export const availText = (it: Pick<InvItem, 'available' | 'unlimited'>) => (it.unlimited ? '不限' : it.available.toLocaleString('zh-CN'))
