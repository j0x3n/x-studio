// 财务模块类型定义与工具函数
// 金额在传输与持久化中统一使用整数分（cents），页面输入展示为元。

export type AccountKind = 'bank' | 'alipay' | 'shopee' | 'cash'

export const ACCOUNT_KIND_LABELS: Record<AccountKind, string> = {
  bank: '银行账户',
  alipay: '支付宝',
  shopee: 'Shopee钱包',
  cash: '现金'
}

export type FinanceAccount = {
  id: number
  name: string
  kind: AccountKind
  currency: string
  shop_id: number | null
  shop_name: string
  opening_balance: number
  balance: number
  archived: boolean
  created_at: string
  updated_at: string
}

export type FinanceEntry = {
  id: number
  account_id: number
  account_name: string
  direction: 'in' | 'out'
  amount: number
  currency: string
  category: string
  note: string
  occurred_at: string
  shop_id: number | null
  source_type: 'manual' | 'payment' | 'expense' | string
  source_id: number
  created_at: string
}

export type FinancePayable = {
  purchase_id: number
  no: string
  supplier_id: number
  supplier_name: string
  status: string
  currency: string
  ordered_at: string
  goods_amount: number
  shipping_fee: number
  order_amount: number
  rejected_amount: number
  paid_amount: number
  payable: number
}

export type FinancePayment = {
  id: number
  purchase_id: number
  purchase_no?: string
  supplier_id: number
  supplier_name?: string
  account_id: number
  amount: number
  currency: string
  note: string
  occurred_at: string
  entry_id: number
  created_at: string
}

export type StatementTotal = {
  currency: string
  purchase_amount: number
  rejected_amount: number
  purchase_payable: number
  paid_in_period: number
}

export type SupplierStatement = {
  supplier: { id: number; name: string; archived: boolean }
  from: string
  to: string
  purchases: FinancePayable[]
  payments: FinancePayment[]
  totals: StatementTotal[]
}

export type FinanceExpense = {
  id: number
  shop_id: number | null
  shop_name: string
  account_id: number
  amount: number
  currency: string
  category: string
  note: string
  occurred_at: string
  entry_id: number
  created_at: string
}

export type FinanceRate = {
  month: string
  currency: string
  rate_micros: number
  updated_at: string
}

export type ReconcileRow = {
  order_no: string
  amount: number
  fee: number
  shipping_insurance: number
  refund: number
  settled_at: string
}

export type ReconcileStatus = 'matched' | 'unmatched' | 'ambiguous' | 'currency' | 'canceled' | 'duplicate'

export const RECONCILE_STATUS_CONFIG: Record<ReconcileStatus, { label: string; pill: string }> = {
  matched: { label: '已匹配', pill: 'pill-success' },
  unmatched: { label: '未匹配', pill: 'pill-danger' },
  ambiguous: { label: '单号歧义', pill: 'pill-warning' },
  currency: { label: '币种不一致', pill: 'pill-warning' },
  canceled: { label: '订单已取消', pill: 'pill-neutral' },
  duplicate: { label: '重复行', pill: 'pill-neutral' }
}

export type ReconcileView = {
  order_no: string
  amount: number
  fee: number
  shipping_insurance: number
  refund: number
  settled_at: string
  status: ReconcileStatus
  reason: string
  order_id: number | null
  system_no: string
  platform_no: string
  system_amount: number | null
  system_refund: number | null
  diff_amount: number | null
  diff_refund: number | null
}

export type ReconcileSummary = {
  total: number
  matched: number
  unmatched: number
  ambiguous: number
  currency: number
  canceled: number
  duplicate: number
  difference: number
}

export type ReconcileBatch = {
  id: number
  shop_id: number
  shop_name: string
  currency: string
  row_count: number
  inserted: number
  skipped: number
  matched: number
  created_at: string
}

// 金额与格式化工具函数
export function formatFen(fen: number | null | undefined, currency = 'CNY'): string {
  if (fen === null || fen === undefined || isNaN(fen)) return '—'
  const yuan = fen / 100
  const formatted = yuan.toLocaleString('zh-CN', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2
  })
  return `${currency} ${formatted}`
}

export function fenToYuan(fen: number | null | undefined): string {
  if (fen === null || fen === undefined || isNaN(fen)) return ''
  return (fen / 100).toFixed(2)
}

export function yuanToFen(yuanStr: string): { fen: number; error?: string } {
  const clean = yuanStr.trim()
  if (!clean) return { fen: 0, error: '请输入金额' }
  if (!/^\d+(?:\.\d{1,2})?$/.test(clean)) return { fen: 0, error: '金额请填写非负数字，最多两位小数' }
  const num = Number(clean)
  if (isNaN(num) || num < 0) return { fen: 0, error: '金额必须为大于等于 0 的有效数字' }
  if (num > 1_000_000_000) return { fen: 0, error: '金额超出可处理范围' }
  const [integer, fraction = ''] = clean.split('.')
  return { fen: Number(integer) * 100 + Number(fraction.padEnd(2, '0')) }
}

export const financeNowInput = () => new Date(Date.now() + 8 * 3600_000).toISOString().slice(0, 16)
export const financeInputUTC = (value: string) => new Date(value + '+08:00').toISOString()
export const financeTime = (value: string) => value ? new Date(value).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai', hour12: false }) : '—'

export function formatRateMicros(micros: number | null | undefined): string {
  if (micros === null || micros === undefined || isNaN(micros)) return '—'
  return (micros / 1_000_000).toFixed(6).replace(/\.?0+$/, '')
}

export function rateToMicros(rateStr: string): { micros: number; error?: string } {
  const clean = rateStr.trim()
  if (!clean) return { micros: 0, error: '请输入汇率' }
  const num = Number(clean)
  if (isNaN(num) || num <= 0) return { micros: 0, error: '汇率必须为大于 0 的有效数字' }
  if (num > 1_000_000_000) return { micros: 0, error: '汇率超出可处理范围' }
  return { micros: Math.round(num * 1_000_000) }
}

export function newRequestKey(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID()
  }
  return 'req-' + Date.now().toString(36) + '-' + Math.random().toString(36).slice(2, 10)
}

// 通用 CSV 下载模板
export function downloadReconcileTemplate() {
  const header = '订单号,金额,平台费用,运费险,退款,结算日期\n'
  const examples = [
    'TB202610200001,159.00,8.50,1.50,0.00,2026-10-20',
    'TB202610200002,299.90,15.00,2.00,50.00,2026-10-20',
    'TB202610200003,88.00,4.40,0.00,0.00,2026-10-20'
  ].join('\n')
  const content = '\uFEFF' + header + examples + '\n'
  const blob = new Blob([content], { type: 'text/csv;charset=utf-8;' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = '通用对账单模板_金额元.csv'
  a.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

// RFC 4180 标准 CSV 解析器：支持 UTF-8 BOM、逗号、双引号转义、跨行字段
export function parseCSVMatrix(text: string): string[][] {
  let cleanText = text
  if (cleanText.charCodeAt(0) === 0xFEFF) {
    cleanText = cleanText.slice(1)
  }

  const rows: string[][] = []
  let currentRow: string[] = []
  let currentField = ''
  let inQuotes = false
  let afterQuote = false

  for (let i = 0; i < cleanText.length; i++) {
    const char = cleanText[i]
    const nextChar = cleanText[i + 1]

    if (inQuotes) {
      if (char === '"') {
        if (nextChar === '"') {
          currentField += '"'
          i++
        } else {
          inQuotes = false
          afterQuote = true
        }
      } else {
        currentField += char
      }
    } else {
      if (char === '"') {
        if (afterQuote || currentField !== '') throw new Error('CSV 双引号只能出现在字段开头')
        inQuotes = true
      } else if (char === ',') {
        currentRow.push(currentField)
        currentField = ''
        afterQuote = false
      } else if (char === '\r') {
        if (nextChar === '\n') {
          i++
        }
        currentRow.push(currentField)
        currentField = ''
        rows.push(currentRow)
        currentRow = []
        afterQuote = false
      } else if (char === '\n') {
        currentRow.push(currentField)
        currentField = ''
        rows.push(currentRow)
        currentRow = []
        afterQuote = false
      } else {
        if (afterQuote) {
          if (char === ' ' || char === '\t') continue
          throw new Error('CSV 双引号结束后只能跟逗号或换行')
        }
        currentField += char
      }
    }
  }

  if (currentField || currentRow.length > 0) {
    currentRow.push(currentField)
    rows.push(currentRow)
  }

  if (inQuotes) throw new Error('CSV 双引号没有正确闭合')

  return rows.filter(r => r.some(col => col.trim() !== ''))
}

// 解析通用 CSV 对账单内容为 ReconcileRow[]
export function parseReconcileCSV(csvText: string): { rows?: ReconcileRow[]; error?: string } {
  if (!csvText || !csvText.trim()) {
    return { error: '请提供 CSV 文本内容或上传 CSV 文件' }
  }

  let matrix: string[][]
  try { matrix = parseCSVMatrix(csvText) } catch (e) { return { error: e instanceof Error ? e.message : 'CSV 格式不正确' } }
  if (matrix.length < 2) {
    return { error: 'CSV 文件缺少表头或数据行' }
  }

  const header = matrix[0].map(h => h.trim().replace(/^["']|["']$/g, ''))
  if (new Set(header).size !== header.length) return { error: 'CSV 表头有重复列名，请使用标准模板' }
  const colIndex = {
    order_no: header.indexOf('订单号'),
    amount: header.indexOf('金额'),
    fee: header.indexOf('平台费用'),
    shipping_insurance: header.indexOf('运费险'),
    refund: header.indexOf('退款'),
    settled_at: header.indexOf('结算日期')
  }

  const missing: string[] = []
  if (colIndex.order_no === -1) missing.push('订单号')
  if (colIndex.amount === -1) missing.push('金额')
  if (colIndex.fee === -1) missing.push('平台费用')
  if (colIndex.shipping_insurance === -1) missing.push('运费险')
  if (colIndex.refund === -1) missing.push('退款')
  if (colIndex.settled_at === -1) missing.push('结算日期')

  if (missing.length > 0) {
    return { error: `CSV 表头缺失必需列：${missing.join('、')}。请下载标准模板比对。` }
  }

  const dataRows = matrix.slice(1)
  if (dataRows.length < 1) {
    return { error: 'CSV 没有有效数据行（需 1–1000 行）' }
  }
  if (dataRows.length > 1000) {
    return { error: `CSV 数据行超出限制（最多 1000 行，当前共 ${dataRows.length} 行）` }
  }

  const parsedRows: ReconcileRow[] = []

  for (let r = 0; r < dataRows.length; r++) {
    const lineNum = r + 2 // 包含表头行
    const row = dataRows[r]
    if (row.length !== header.length) return { error: `第 ${lineNum} 行列数与表头不一致，请检查逗号和双引号` }

    const orderNo = (row[colIndex.order_no] || '').trim()
    if (!orderNo) {
      return { error: `第 ${lineNum} 行：订单号不能为空` }
    }
    if (orderNo.length > 100) {
      return { error: `第 ${lineNum} 行：订单号长度不能超过 100 字符` }
    }

    const parseMoneyField = (valStr: string | undefined, _name: string): number | null => {
      const s = (valStr || '').trim().replace(/,/g, '')
      const result = yuanToFen(s)
      return result.error ? null : result.fen
    }

    const amount = parseMoneyField(row[colIndex.amount], '金额')
    if (amount === null) {
      return { error: `第 ${lineNum} 行：金额必须为大于等于 0 的有效数字` }
    }

    const fee = parseMoneyField(row[colIndex.fee], '平台费用')
    if (fee === null) {
      return { error: `第 ${lineNum} 行：平台费用必须为大于等于 0 的有效数字` }
    }

    const shippingInsurance = parseMoneyField(row[colIndex.shipping_insurance], '运费险')
    if (shippingInsurance === null) {
      return { error: `第 ${lineNum} 行：运费险必须为大于等于 0 的有效数字` }
    }

    const refund = parseMoneyField(row[colIndex.refund], '退款')
    if (refund === null) {
      return { error: `第 ${lineNum} 行：退款必须为大于等于 0 的有效数字` }
    }

    let settledAt = (row[colIndex.settled_at] || '').trim()
    if (!settledAt) {
      return { error: `第 ${lineNum} 行：结算日期不能为空` }
    }
    // 标准化 YYYY/MM/DD 到 YYYY-MM-DD
    settledAt = settledAt.replace(/\//g, '-')
    if (!/^\d{4}-\d{2}-\d{2}$/.test(settledAt)) {
      return { error: `第 ${lineNum} 行：结算日期格式非法（需为 YYYY-MM-DD，如 2026-10-20）` }
    }
    const parsedDate = new Date(settledAt + 'T00:00:00+08:00')
    if (Number.isNaN(parsedDate.getTime()) || new Date(parsedDate.getTime() + 8 * 3600_000).toISOString().slice(0, 10) !== settledAt) return { error: `第 ${lineNum} 行：结算日期不存在` }

    parsedRows.push({
      order_no: orderNo,
      amount,
      fee,
      shipping_insurance: shippingInsurance,
      refund,
      settled_at: settledAt
    })
  }

  return { rows: parsedRows }
}
