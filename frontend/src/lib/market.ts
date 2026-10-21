// 数据与运营（market）的类型、中文名、接口和浏览器端 CSV 解析。
// 金额是整数分，比例是整数万分比（bps）。localStorage xs-mock=1 时走假数据。
import { api, qs, type Page } from './api'
import { mockOn } from './creative'

export type Keyword = { word: string; category: string; date: string; search_volume: number | null; clicks: number | null; ctr_bps: number | null; cvr_bps: number | null; competition: number | null; score: number | null; trend: { date: string; search_volume: number | null }[] }
export type Competitor = { platform_item_id: string; keyword: string; rank: number | null; title: string; price_cents: number | null; sales_text: string | null; sales: number | null; shop_name: string; image_url: string; material: string; date: string }
export type AdRow = { date: string; campaign: string; keyword: string; listing_id: number | null; cost_cents: number | null; impressions: number | null; clicks: number | null; orders: number | null; gmv_cents: number | null }
export type Snapshot = { id: number; kind: string; source: 'script' | 'csv'; source_url: string; captured_at: string; rows: number; created_by: number; created_at: string }
export type Token = { id: number; name: string; prefix: string; scopes: string[]; created_at: string; last_used_at: string; revoked_at: string }
export type IngestResult = { snapshot_id: number; duplicate: boolean; inserted: number; updated: number; skipped: { index: number; reason: string }[] }
export type Preview = { preview_id: string; total: number; ok: number; errors: { index: number; reason: string }[]; sample: Record<string, unknown>[] }
export type Level = 'info' | 'warn' | 'action'
export type Advice = { id: number; shop_id: number; listing_id: number | null; rule: string; keyword: string; level: Level; facts: string[]; guesses: string[]; missing: string[]; explain: string; status: 'open' | 'done' | 'ignored' | 'expired'; task_id: number | null; created_at: string; updated_at: string; product_id?: number; product_name?: string; hero_b_item_id?: number }

export const KIND: Record<string, string> = { keyword: '关键词', search: '搜索结果', item: '商品页', my_listing: '我的商品', ad: '推广' }
export const MATERIALS = ['淡水', '天然', '施家', '仿', '贝珠', '未写']
export const RULE: Record<string, string> = { breakeven: '保本出价', no_paid: '不建议付费', baroque: '巴洛克以自然搜索为主', new_listing: '新品期', stop_word: '关键词不出单', stop_word_cost: '关键词花费超毛利', raise_budget: '可以加预算', hero_b: '首图点击率偏低' }
export const LEVEL: Record<Level, { label: string; pill: string }> = { action: { label: '建议操作', pill: 'pill-danger' }, warn: { label: '注意', pill: 'pill-warning' }, info: { label: '参考', pill: 'pill-neutral' } }

export const yuan = (c: number | null | undefined) => (typeof c === 'number' ? '¥' + (c / 100).toFixed(2) : '—')
export const bps = (b: number | null | undefined) => (typeof b === 'number' ? (b / 100).toFixed(2) + '%' : '—')
export const n = (v: number | null | undefined) => (typeof v === 'number' ? v.toLocaleString('zh-CN') : '—')

const wait = <T,>(v: T) => new Promise<T>(r => setTimeout(() => r(structuredClone(v)), 200))
const today = () => new Date(Date.now() + 8 * 3600_000).toISOString().slice(0, 10)
const day = (k: number) => new Date(Date.now() + 8 * 3600_000 - k * 86400_000).toISOString().slice(0, 10)

export const marketApi = {
  keywords: (p: { shop_id: number; category?: string; q?: string; days?: number; sort?: string; page?: number }) => mockOn() ? wait({ items: MOCK_KW, total: MOCK_KW.length }) : api<Page<Keyword>>('/api/market/keywords' + qs(p)),
  competitors: (p: { shop_id: number; keyword?: string; material?: string; days?: number; page?: number }) => mockOn() ? wait({ items: MOCK_COMP, total: MOCK_COMP.length }) : api<Page<Competitor>>('/api/market/competitors' + qs(p)),
  ads: (p: { shop_id: number; from?: string; to?: string; listing_id?: number }) => mockOn() ? wait({ items: MOCK_ADS, total: MOCK_ADS.length }) : api<Page<AdRow>>('/api/market/ads' + qs(p)),
  snapshots: (p: { shop_id: number; kind?: string; page?: number }) => mockOn() ? wait({ items: MOCK_SNAP, total: MOCK_SNAP.length }) : api<Page<Snapshot>>('/api/market/snapshots' + qs(p)),
  shop: (id: number) => mockOn() ? wait({ daily_budget_cents: 3000 as number | null }) : api<{ daily_budget_cents: number | null }>(`/api/market/shops/${id}/settings`),
  saveShop: (id: number, daily_budget_cents: number | null) => mockOn() ? wait({}) : api(`/api/market/shops/${id}/settings`, { method: 'PUT', body: { daily_budget_cents } }),
  tokens: () => mockOn() ? wait({ items: MOCK_TOKENS, total: 1 }) : api<Page<Token>>('/api/market/tokens'),
  createToken: (name: string) => mockOn() ? wait({ id: 9, token: 'xst_mock_only_not_a_real_token' }) : api<{ id: number; token: string }>('/api/market/tokens', { body: { name } }),
  revokeToken: (id: number) => mockOn() ? wait({}) : api(`/api/market/tokens/${id}`, { method: 'DELETE' }),
  preview: (body: { shop_id: number; kind: 'my_listing' | 'ad'; rows: Record<string, unknown>[] }) => mockOn() ? wait<Preview>({ preview_id: 'p1', total: body.rows.length, ok: Math.max(0, body.rows.length - 1), errors: body.rows.length ? [{ index: 0, reason: '商品 id 对不上上架版本，去上架版本里填商品链接' }] : [], sample: body.rows.slice(0, 50) }) : api<Preview>('/api/market/import/preview', { body }),
  confirm: (preview_id: string) => mockOn() ? wait<IngestResult>({ snapshot_id: 1, duplicate: false, inserted: 12, updated: 0, skipped: [] }) : api<IngestResult>('/api/market/import/confirm', { body: { preview_id } }),
  advice: (p: { shop_id?: number; listing_id?: number; status?: string; page?: number }) => mockOn() ? wait({ items: MOCK_ADVICE.filter(a => !p.status || a.status === p.status), total: MOCK_ADVICE.length }) : api<Page<Advice>>('/api/market/advice' + qs(p)),
  recompute: (shop_id: number) => mockOn() ? wait({}) : api('/api/market/advice/recompute', { body: { shop_id } }),
  explain: (id: number) => mockOn() ? wait({ explain: '这款售价 69 元，低于付费门槛 99 元。按一单毛利算，付费推广很难回本，建议先把标题和首图做好，靠自然搜索出单。7 天后看自然搜索访客有没有涨。' }) : api<{ explain: string }>(`/api/market/advice/${id}/explain`, { method: 'POST' }),
  patchAdvice: (id: number, body: { status?: 'done' | 'ignored'; task_id?: number }) => mockOn() ? wait({}) : api(`/api/market/advice/${id}`, { method: 'PATCH', body })
}

// ---------- CSV：浏览器里解码（生意参谋、万相台导出常是 GBK），再按表头映射成 2.1 节的字段 ----------
export async function readCsv(file: File): Promise<string[][]> {
  const buf = await file.arrayBuffer()
  let text = new TextDecoder('utf-8', { fatal: false }).decode(buf)
  // UTF-8 解出很多替换符，多半是 GBK
  if ((text.match(/�/g) || []).length > 3) text = new TextDecoder('gbk').decode(buf)
  return parseCsv(text.replace(/^﻿/, ''))
}
export function parseCsv(text: string): string[][] {
  const rows: string[][] = []
  let row: string[] = [], cell = '', q = false
  for (let i = 0; i < text.length; i++) {
    const c = text[i]
    if (q) {
      if (c === '"' && text[i + 1] === '"') { cell += '"'; i++ }
      else if (c === '"') q = false
      else cell += c
    } else if (c === '"') q = true
    else if (c === ',' || c === '\t') { row.push(cell); cell = '' }
    else if (c === '\n' || c === '\r') {
      if (c === '\r' && text[i + 1] === '\n') i++
      row.push(cell); cell = ''
      if (row.some(x => x.trim() !== '')) rows.push(row.map(x => x.trim()))
      row = []
    } else cell += c
  }
  row.push(cell)
  if (row.some(x => x.trim() !== '')) rows.push(row.map(x => x.trim()))
  return rows
}

// 每种报表要的字段和常见表头写法（按顺序猜，猜错了页面上可以改）
export const IMPORT_FIELDS: Record<'my_listing' | 'ad', { key: string; label: string; num?: 'int' | 'money'; alias: string[]; required?: boolean }[]> = {
  my_listing: [
    { key: 'date', label: '日期', alias: ['统计日期', '日期', '时间'], required: true },
    { key: 'item_id', label: '商品 ID', alias: ['商品ID', '商品id', '宝贝ID', '商品编号'], required: true },
    { key: 'impressions', label: '曝光', num: 'int', alias: ['曝光量', '展现量', '商品曝光'] },
    { key: 'uv', label: '访客', num: 'int', alias: ['商品访客数', '访客数', 'UV'] },
    { key: 'clicks', label: '点击', num: 'int', alias: ['点击量', '点击次数', '商品浏览量', '浏览量'] },
    { key: 'add_cart', label: '加购', num: 'int', alias: ['商品加购件数', '加购件数', '加购人数'] },
    { key: 'orders', label: '成交', num: 'int', alias: ['支付件数', '支付买家数', '成交件数', '支付子订单数'] },
    { key: 'gmv', label: '成交额（元）', num: 'money', alias: ['支付金额', '成交金额', '销售额'] }
  ],
  ad: [
    { key: 'date', label: '日期', alias: ['日期', '统计日期', '时间'], required: true },
    { key: 'campaign', label: '计划', alias: ['计划名称', '计划名字', '推广计划'] },
    { key: 'keyword', label: '关键词', alias: ['关键词', '关键词名称', '词'] },
    { key: 'item_id', label: '商品 ID', alias: ['商品ID', '宝贝ID', '主体ID'] },
    { key: 'cost', label: '花费（元）', num: 'money', alias: ['花费', '消耗', '总花费'], required: true },
    { key: 'impressions', label: '展现', num: 'int', alias: ['展现量', '曝光量'] },
    { key: 'clicks', label: '点击', num: 'int', alias: ['点击量', '点击数'] },
    { key: 'orders', label: '成交笔数', num: 'int', alias: ['总成交笔数', '成交笔数', '直接成交笔数'] },
    { key: 'gmv', label: '成交额（元）', num: 'money', alias: ['总成交金额', '成交金额', '直接成交金额'] }
  ]
}
export function guessMap(kind: 'my_listing' | 'ad', header: string[]) {
  const m: Record<string, number> = {}
  for (const f of IMPORT_FIELDS[kind]) {
    const i = f.alias.map(a => header.findIndex(h => h.replace(/\s/g, '') === a)).find(i => i >= 0) ?? header.findIndex(h => f.alias.some(a => h.includes(a)))
    m[f.key] = i ?? -1
  }
  return m
}
// 表格单元格 → 2.1 节的值：数字去掉千分位，百分号和「-」当空；日期统一成 YYYY-MM-DD；元保持字符串交给后端做整数换算
export function mapRows(kind: 'my_listing' | 'ad', rows: string[][], map: Record<string, number>) {
  return rows.map(r => {
    const o: Record<string, unknown> = {}
    for (const f of IMPORT_FIELDS[kind]) {
      const i = map[f.key]
      let v: string | null = i >= 0 ? (r[i] ?? '').trim() : null
      if (v === '' || v === '-' || v === '--') v = null
      if (v !== null && f.num) v = v.replace(/[,，¥￥\s]/g, '')
      if (f.key === 'date' && v) { const d = v.replace(/[年/.]/g, '-').replace(/月/, '-').replace(/日/, '').match(/^(\d{4})-(\d{1,2})-(\d{1,2})/); v = d ? `${d[1]}-${d[2].padStart(2, '0')}-${d[3].padStart(2, '0')}` : v }
      o[f.key] = v === null ? null : f.num === 'int' ? (/^\d+$/.test(v) ? Number(v) : v) : v
    }
    return o
  })
}

// ---------- 假数据 ----------
const MOCK_KW: Keyword[] = [
  { word: '淡水珍珠耳钉', category: 'pearl', date: today(), search_volume: 58200, clicks: 3100, ctr_bps: 532, cvr_bps: 412, competition: 82, score: 2398, trend: [6, 5, 4, 3, 2, 1, 0].map(k => ({ date: day(k), search_volume: 52000 + k * 900 })) },
  { word: '珍珠耳钉女小众', category: 'pearl', date: today(), search_volume: 21300, clicks: 1460, ctr_bps: 685, cvr_bps: 515, competition: 46, score: 1097, trend: [6, 5, 4, 3, 2, 1, 0].map(k => ({ date: day(k), search_volume: 19000 + (6 - k) * 400 })) },
  { word: '巴洛克珍珠项链', category: 'pearl', date: today(), search_volume: 33800, clicks: 2050, ctr_bps: 606, cvr_bps: 39, competition: 71, score: 132, trend: [] },
  { word: '爱迪生珍珠', category: 'pearl', date: day(3), search_volume: 9100, clicks: null, ctr_bps: null, cvr_bps: null, competition: null, score: null, trend: [] }
]
const MOCK_COMP: Competitor[] = [
  { platform_item_id: '7312', keyword: '淡水珍珠耳钉', rank: 1, title: '真淡水珍珠耳钉女 925银针 小众高级感', price_cents: 8900, sales_text: '月销 2000+', sales: 2000, shop_name: '某珠宝店', image_url: '', material: '淡水', date: today() },
  { platform_item_id: '7313', keyword: '淡水珍珠耳钉', rank: 2, title: '施家珍珠耳钉 法式复古', price_cents: 3900, sales_text: '月销 1.2万+', sales: 12000, shop_name: '某饰品店', image_url: '', material: '施家', date: today() },
  { platform_item_id: '7314', keyword: '淡水珍珠耳钉', rank: 3, title: '珍珠耳环 气质简约', price_cents: 1990, sales_text: '', sales: null, shop_name: '某店', image_url: '', material: '未写', date: today() }
]
const MOCK_ADS: AdRow[] = [
  { date: day(1), campaign: '新品测词', keyword: '珍珠耳钉女小众', listing_id: 1, cost_cents: 2350, impressions: 4100, clicks: 62, orders: 1, gmv_cents: 12900 },
  { date: day(1), campaign: '新品测词', keyword: '淡水珍珠耳钉', listing_id: 1, cost_cents: 4800, impressions: 9200, clicks: 95, orders: 0, gmv_cents: 0 }
]
const MOCK_SNAP: Snapshot[] = [
  { id: 3, kind: 'keyword', source: 'script', source_url: 'https://…', captured_at: new Date().toISOString(), rows: 120, created_by: 1, created_at: new Date().toISOString() },
  { id: 2, kind: 'ad', source: 'csv', source_url: '', captured_at: new Date(Date.now() - 86400_000).toISOString(), rows: 36, created_by: 1, created_at: new Date(Date.now() - 86400_000).toISOString() }
]
const MOCK_TOKENS: Token[] = [{ id: 1, name: '办公室 Chrome', prefix: 'xst_ab12', scopes: ['market.ingest'], created_at: new Date(Date.now() - 5 * 86400_000).toISOString(), last_used_at: new Date().toISOString(), revoked_at: '' }]
const MOCK_ADVICE: Advice[] = [
  { id: 1, shop_id: 1, listing_id: 1, rule: 'stop_word', keyword: '淡水珍珠耳钉', level: 'action', facts: ['近 30 天点击 95 次，成交 0 单', '这款转化率 2.10%，按规则点击达到 72 次还没成交就该处理', '花费 ¥48.00'], guesses: ['这个词太泛，进来的人多数在比价'], missing: [], explain: '', status: 'open', task_id: null, created_at: new Date().toISOString(), updated_at: new Date().toISOString(), product_id: 1, product_name: '强光爱迪生蝴蝶结耳钉' },
  { id: 2, shop_id: 1, listing_id: 1, rule: 'breakeven', keyword: '', level: 'info', facts: ['售价 ¥129.00，毛利 ¥71.00', '转化率 2.10%', '保本单次点击价 = ¥1.49；保本 ROI = 1.82'], guesses: [], missing: [], explain: '', status: 'open', task_id: null, created_at: new Date().toISOString(), updated_at: new Date().toISOString(), product_id: 1, product_name: '强光爱迪生蝴蝶结耳钉' },
  { id: 3, shop_id: 1, listing_id: 2, rule: 'new_listing', keyword: '', level: 'warn', facts: ['上架第 5 天'], guesses: ['新品期先测中长尾词'], missing: ['缺店铺日预算（在数据页填）'], explain: '', status: 'open', task_id: null, created_at: new Date().toISOString(), updated_at: new Date().toISOString(), product_id: 2, product_name: '初雪 · 小米珠颈链' },
  { id: 4, shop_id: 1, listing_id: 2, rule: 'hero_b', keyword: '', level: 'warn', facts: ['近 7 天曝光 1,820，点击率 1.10%', '店内平均点击率 2.40%，低于平均的 70%'], guesses: ['首图在搜索结果里不够显眼'], missing: [], explain: '', status: 'open', task_id: null, created_at: new Date().toISOString(), updated_at: new Date().toISOString(), product_id: 2, product_name: '初雪 · 小米珠颈链', hero_b_item_id: 2 }
]
