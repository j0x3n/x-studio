// 进销存单据：字段与后端 JSON 一致，金额以分记。
export type Supplier = { id: number; name: string; contact: string; phone: string; wechat: string; address: string; products: string; settlement: 'cash' | 'monthly'; payment_days: number; note: string; archived: boolean; updated_at: string }
export type PurchaseStatus = 'draft' | 'ordered' | 'partial' | 'received' | 'done' | 'canceled'
export type PurchaseLine = { id: number; item_id: number; code: string; name: string; unit: string; qty: number; amount: number; shipping_share: number; received_qty: number; rejected_qty: number }
export type Receipt = { id: number; warehouse_id: number; warehouse_name: string; received_at: string; note: string; lines: { line_id: number; qty: number; rejected: number; grades: { grade: string; qty: number }[]; batch_ids: number[] }[] }
export type PurchaseReturn = { id: number; line_id: number; qty: number; amount: number; status: 'pending' | 'done'; note: string; created_at: string; done_at: string }
export type Purchase = { id: number; no: string; supplier_id: number; supplier_name: string; status: PurchaseStatus; currency: string; shipping_fee: number; goods_amount?: number; expected_at: string; product_id: number | null; is_sample: boolean; note: string; created_at: string; ordered_at: string; done_at: string; lines: PurchaseLine[]; receipts: Receipt[]; returns: PurchaseReturn[] }
export type OrderStatus = 'paid' | 'shipped' | 'canceled'
export type Pick = { id: number; warehouse_id: number; batch_id: number | null; qty: number; cost?: number; currency: string; grade: string }
export type OrderLine = { id: number; item_id: number; code: string; title: string; qty: number; price: number; amount: number; returned_qty: number; picks: Pick[] }
export type ReturnAction = 'restock' | 'scrap' | 'none'
export type OrderReturn = { id: number; refund_amount: number; reason: string; created_at: string; lines: { line_id: number; qty: number; action: ReturnAction; warehouse_id: number | null; reason_code?: string; creative_set_id?: number | null }[] }
export type Order = { id: number; no: string; shop_id: number; shop_name?: string; platform_no: string; source: string; status: OrderStatus; short: boolean; buyer_name: string; buyer_phone: string; address: string; buyer_note: string; seller_note: string; currency: string; goods_amount: number; shipping_fee: number; discount: number; paid_amount: number; paid_at: string; shipped_at: string; canceled_at: string; carrier: string; tracking_no: string; created_at: string; lines: OrderLine[]; returns: OrderReturn[] }
export const purchaseStatus: Record<PurchaseStatus, { label: string; pill: string }> = {
  draft: { label: '草稿', pill: 'pill-neutral' }, ordered: { label: '已下单', pill: 'pill-info' }, partial: { label: '部分到货', pill: 'pill-warning' }, received: { label: '全部到货', pill: 'pill-success' }, done: { label: '已完成', pill: 'pill-neutral' }, canceled: { label: '已取消', pill: 'pill-neutral' }
}
export const orderStatus: Record<OrderStatus, { label: string; pill: string }> = { paid: { label: '待发货', pill: 'pill-warning' }, shipped: { label: '已发货', pill: 'pill-success' }, canceled: { label: '已取消', pill: 'pill-neutral' } }
export const returnAction: Record<ReturnAction, string> = { restock: '退货回仓', scrap: '退回报损', none: '不退货退款' }
// 售后行的退货原因，回流到 AI 出图：同款同原因累计到阈值会建议改对应的图
export const returnReason: Record<string, string> = { not_as_pictured: '与图片不符', smaller: '比想象小', color: '颜色不符', flaw: '瑕疵', other: '其他' }
export const chinaDate = (s?: string) => s?.length === 10 ? s : new Intl.DateTimeFormat('sv-SE', { timeZone: 'Asia/Shanghai' }).format(s ? new Date(s) : new Date())
export const money = (fen: number | undefined, currency = 'CNY') => typeof fen === 'number' ? new Intl.NumberFormat('zh-CN', { style: 'currency', currency }).format(fen / 100) : '—'
