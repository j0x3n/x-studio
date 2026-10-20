// 组装加工（assembly）：数据类型、状态字典及损耗与金额计算
export type RecipeLine = {
  id?: number
  recipe_id?: number
  item_id: number
  item_code?: string
  item_name?: string
  item_unit?: string
  qty: number
  loss_bps: number // 0 ~ 10000
}

export type AssemblyRecipe = {
  id: number
  name: string
  output_item_id: number
  output_item_code?: string
  output_item_name?: string
  output_item_unit?: string
  output_qty: number
  currency: string
  labor_cost?: number // 分；无 product.cost / finance.view 权限时不返回
  article_id?: number | null
  article_title?: string | null
  note: string
  archived: boolean
  lines_count?: number
  lines?: RecipeLine[]
  created_by?: number
  created_at: string
  updated_at: string
}

export type AssemblyOrderStatus = 'draft' | 'done' | 'canceled'

export type AssemblyOrderLine = {
  id: number
  order_id: number
  item_id: number
  item_code?: string
  item_name?: string
  item_unit?: string
  unit_qty: number
  loss_bps: number
  required_qty: number
  actual_cost?: number // 分；无 product.cost / finance.view 权限时不返回
  currency: string
}

export type RecipeSnapshot = {
  recipe_id: number
  name: string
  output_item_id: number
  output_item_code: string
  output_item_name: string
  output_item_unit: string
  output_qty: number
  currency: string
  labor_cost?: number
  article_id?: number | null
  note?: string
  lines: {
    item_id: number
    code?: string
    name?: string
    unit?: string
    qty: number
    loss_bps: number
  }[]
}

export type AssemblyOrder = {
  id: number
  no: string
  recipe_id: number
  recipe_name?: string
  runs: number
  warehouse_id: number
  warehouse_name?: string
  status: AssemblyOrderStatus
  output_item_id: number
  output_item_code?: string
  output_item_name?: string
  output_item_unit?: string
  output_qty: number
  currency: string
  labor_cost?: number
  material_cost?: number
  total_cost?: number
  output_batch_id?: number | null
  recipe_snapshot?: RecipeSnapshot
  note: string
  created_by?: number
  created_at: string
  done_at?: string | null
  canceled_at?: string | null
  lines?: AssemblyOrderLine[]
}

export const assemblyStatus: Record<AssemblyOrderStatus, { label: string; pill: string }> = {
  draft: { label: '草稿', pill: 'pill-warning' },
  done: { label: '已完成', pill: 'pill-success' },
  canceled: { label: '已取消', pill: 'pill-neutral' },
}

// 损耗精确 ceil 算法: ceil(qty * runs * (10000 + loss_bps) / 10000)
export function calcRequiredQty(qty: number, runs: number, lossBps: number): number {
  if (qty <= 0 || runs <= 0) return 0
  const bps = Math.max(0, Math.min(10000, lossBps || 0))
  return Math.ceil((qty * runs * (10000 + bps)) / 10000)
}

// 万分比转成百分比字符串显示，例如 500 -> "5%", 25 -> "0.25%"
export function formatBps(lossBps: number | undefined | null): string {
  const bps = lossBps || 0
  const pct = bps / 100
  return `${pct % 1 === 0 ? pct : pct.toFixed(2)}%`
}

// 金额格式化（分 -> 货币格式）
export const formatMoney = (fen?: number | null, currency = 'CNY') => {
  if (typeof fen !== 'number') return '—'
  return new Intl.NumberFormat('zh-CN', { style: 'currency', currency }).format(fen / 100)
}
