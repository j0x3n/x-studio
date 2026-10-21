// AI 出图（creative）的类型、中文名和接口封装。契约见 接口约定-AI出图.md。
// 后端还没合并时，浏览器里 localStorage.setItem('xs-mock', '1') 用假数据看页面。
import { api, qs, type Page } from './api'

export type Check = 'pass' | 'fail' | 'unsure' | 'na'
export type Route = 'auto_pass' | 'auto_redraw' | 'manual' | 'pending'
export type Verdict = 'pass' | 'redraw' | 'discard'
export type ProductMode = 'pixel_lock' | 'redraw' | 'model' | 'none' | 'photo'
export type SetStatus = 'draft' | 'generating' | 'reviewing' | 'paused_limit' | 'ready' | 'exported'

export type Review = {
  checks: { product: Check; size: Check; body: Check; placement: Check; style: Check }
  size_mm: number[]; score: number
  issues: { check: string; box: [number, number, number, number]; reason: string }[]
}
export type Hard = {
  pixel?: { pass: boolean; max_diff: number }
  text?: { key: string; expect: string; found: string; pass: boolean; warn?: boolean; critical?: boolean }[]
  banned?: string[]
  format?: { pass: boolean }
  error?: string
  cutout?: { mode: 'rembg' | 'fallback' | string; coverage: number; code: string }
}
export type CreativeItem = {
  id: number; set_id: number | null; slot: string; seq: number
  template_key: string; template_version: number; attempt: number; superseded: boolean; variant: '' | 'A' | 'B'
  product_mode: ProductMode; source_asset_ids: number[]; model_id: number | null; wear_type: '' | 'necklace' | 'earring' | 'bracelet' | 'ring'
  size_compare: boolean; prompt: string; texts: { key: string; text: string; critical: boolean }[]
  output_asset_id: number | null; output_url?: string; source_urls?: string[]
  hard: Hard | null; review_a: Review | null; review_b: Review | null
  model?: string; model_returned?: string // 实际用的生图模型、AI 原始返回尺寸（照片模式为空）
  route: Route; route_reasons: string[]; risk: number
  human: '' | Verdict; human_note: string; human_by?: number; human_at?: string
  error: string; created_at: string
  // 复核队列多带的字段
  listing_id?: number; product_id?: number; product_name?: string; shop_id?: number
}
export type CreativeSet = {
  id: number; listing_id: number; version: number; status: SetStatus
  template_keys: Record<string, string>; params: SetParams; hero_b_item_id: number | null
  images_used: number; images_allowance: number
  glanced_by: number | null; glanced_at: string; exported_at: string; export_file: string
  created_by: number; created_at: string; updated_at: string
  parts?: ('main' | 'detail')[]; progress?: { total: number; completed: number }
  items: CreativeItem[]
  jobs?: { id: number; kind: string; status: string; progress: number; message: string }[]
  missing?: string[] // 导出还缺什么（后端 409 时给出）
}
export type SetParams = { hero_style?: 'vertical' | 'album' | 'window'; hero_b?: boolean; sizes?: { label: string; mm: number }[]; note?: string; skip_missing?: boolean }
// 新建套装 / 重画 / 试跑前的预检：缺实拍或没配模特时后端 409 给出
export type PreflightMissing = { slot: string; label: string; template_key: string; reason: string }
export const preflightMissing = (data: unknown): PreflightMissing[] => {
  const m = data && typeof data === 'object' ? (data as { missing?: unknown }).missing : null
  return Array.isArray(m) ? m.filter(x => x && typeof x === 'object' && 'reason' in x) as PreflightMissing[] : []
}
export type CreativeTemplate = {
  key: string; name: string; kind: 'main' | 'detail'; slot: string; categories: string[]; purpose: string; prompt: string
  style_refs: string[]; texts: { key: string; from: string; max: number; critical: boolean }[]; photo_codes: string[]
  product_mode: ProductMode; product_box: [number, number, number, number] | null; gen_size: string
  lock_shape?: string; size_compare?: boolean | null; hero_style?: string
  export: { name: string; w: number; h: number }[]; count: number
  wear: { slot: string; body_part: string; framing: string } | null
  version: number; enabled: boolean; source: 'file' | 'custom'; updated_at?: string
}
export type ShopCreative = { models: { female_main?: number | null; male?: number | null; hand?: number | null }; template_set: Record<string, string> }
export type CalibrationRow = { category: string; total: number; agree: number; open: boolean; tightened: boolean; tightened_reason?: string; auto_pass_rate?: number; override_rate?: number; return_links?: number }
export type Suggestion = { id: number; listing_id: number; set_id: number | null; item_id: number | null; slot: string; reason_code: string; count: number; status: 'open' | 'done' | 'ignored'; product_name?: string; created_at: string }
export type Stats = { categories: CalibrationRow[]; suggestions: Suggestion[] }
export type CreativeSettings = {
  final_glance: boolean; max_images_per_listing: number; auto_pass_score: number; manual_score_min: number; max_auto_redraw: number
  calibration_count: number; calibration_agreement: number; spot_check_rate: number; size_tolerance: number; size_compare_tolerance: number; return_reason_threshold: number
}

export const SLOT: Record<string, string> = {
  main1: '首图', main2: '上身', main3: '细节', main4: '尺寸', main5: '规格包装', white: '白底', sku: 'SKU',
  d1: '首屏', d2: '真假对比', d3: '细节', d4: '上身', d5: '尺寸参数', d6: '同系列', d7: '包装', d8: '保养售后', d9: '关于我们'
}
export const MAIN_SLOTS = ['main1', 'main2', 'main3', 'main4', 'main5', 'sku']
export const DETAIL_SLOTS = ['d1', 'd2', 'd3', 'd4', 'd5', 'd6', 'd7', 'd8', 'd9']
export const slotName = (s: string) => SLOT[s] || s
export const MODE: Record<ProductMode, string> = { pixel_lock: '像素锁定', redraw: '参考图重画', model: '模特', none: '不放商品', photo: '实拍原图' }
export const WEAR: Record<string, string> = { necklace: '项链', earring: '耳饰', bracelet: '手链', ring: '戒指' }
export const CHECK_NAME: Record<string, string> = { product: '商品还原', size: '尺寸比例', body: '人体自然', placement: '佩戴位置', style: '视觉规范' }
export const ROUTE: Record<Route, { label: string; pill: string }> = {
  auto_pass: { label: '自动通过', pill: 'pill-success' }, auto_redraw: { label: '自动重画', pill: 'pill-warning' },
  manual: { label: '待人工', pill: 'pill-info' }, pending: { label: '复核中', pill: 'pill-neutral' }
}
export const VERDICT: Record<Verdict, { label: string; pill: string }> = {
  pass: { label: '人工通过', pill: 'pill-success' }, redraw: { label: '要求重画', pill: 'pill-warning' }, discard: { label: '已弃用', pill: 'pill-neutral' }
}
export const SET_STATUS: Record<SetStatus, { label: string; pill: string }> = {
  draft: { label: '草稿', pill: 'pill-neutral' }, generating: { label: '生成中', pill: 'pill-info' }, reviewing: { label: '复核中', pill: 'pill-purple' },
  paused_limit: { label: '到上限暂停', pill: 'pill-warning' }, ready: { label: '可导出', pill: 'pill-success' }, exported: { label: '已导出', pill: 'pill-teal' }
}
export const HERO_STYLE = { vertical: '竖排大字', album: '米白画册', window: '窗边氛围' } as const
export const REASON: Record<string, string> = { not_as_pictured: '与图片不符', smaller: '比想象小', color: '颜色不符', flaw: '瑕疵', other: '其他' }
export const MODEL_SLOT = { female_main: '女主模特（F1）', male: '男模特（M1）', hand: '手部模特' } as const

// 一张图当前的结论：人工判断优先，其次分流
export const itemState = (it: CreativeItem) =>
  it.error ? (it.route_reasons.includes('缺少素材，未生成') ? { label: '缺素材', pill: 'pill-warning' } : { label: '失败', pill: 'pill-danger' }) : it.human ? VERDICT[it.human] : !it.output_url && it.route === 'pending' ? { label: '待生成', pill: 'pill-neutral' } : ROUTE[it.route] || ROUTE.pending
export const itemOk = (it: CreativeItem) => it.human === 'pass' || (!it.human && it.route === 'auto_pass')
export const criticalFail = (it: CreativeItem) => (it.hard?.text || []).some(t => !t.pass && !t.warn)
export const minScore = (it: CreativeItem) => {
  const s = [it.review_a?.score, it.review_b?.score].filter((x): x is number => typeof x === 'number')
  return s.length ? Math.min(...s) : null
}

// ---------- 后端原样返回数据库行：没复核的是 {}、没有的编号是 0，这里统一成上面的类型 ----------
type Raw = Record<string, any> // eslint-disable-line @typescript-eslint/no-explicit-any
const normReview = (r: Raw | null | undefined): Review | null =>
  r && r.checks && Object.keys(r.checks).length ? { checks: r.checks, size_mm: r.size_mm || [], score: r.score ?? 0, issues: r.issues || [] } : null
const normHard = (h: Raw | null | undefined, mode: string, done: boolean): Hard | null => {
  if (!h || !Object.keys(h).length) return null
  return {
    pixel: mode === 'pixel_lock' && done ? h.pixel : undefined, // 非像素锁定的图后端也写 pass:true，不显示
    format: done ? h.format : undefined, text: h.text || [], banned: h.banned || [], error: h.error || undefined,
    cutout: h.cutout && h.cutout.mode ? h.cutout : undefined
  }
}
export const normItem = (it: Raw): CreativeItem => ({
  ...it, model_id: it.model_id || null, output_asset_id: it.output_asset_id || null,
  source_asset_ids: it.source_asset_ids || [], source_urls: it.source_urls || [], output_url: it.output_url || undefined,
  texts: it.texts || [], route_reasons: it.route_reasons || [], human_note: it.human_note || '', error: it.error || '',
  review_a: normReview(it.review_a), review_b: normReview(it.review_b), hard: normHard(it.hard, it.product_mode, !!it.output_asset_id)
} as CreativeItem)
const normSet = (s: Raw): CreativeSet => ({ ...s, params: s.params || {}, template_keys: s.template_keys || {}, items: (s.items || []).map(normItem), jobs: s.jobs || [] } as CreativeSet)
const normTemplate = (t: Raw): CreativeTemplate => ({
  ...t, categories: t.categories || [], style_refs: t.style_refs || [], texts: t.texts || [], photo_codes: t.photo_codes || [], export: t.export || [],
  product_box: t.product_box?.length === 4 ? t.product_box : null
} as CreativeTemplate)
const page = <T,>(f: (x: Raw) => T) => (p: Page<Raw>): Page<T> => ({ ...p, items: (p.items || []).map(f) })
// 模板保存只发改过的、后端允许改的字段
export const TEMPLATE_FIELDS = ['name', 'kind', 'slot', 'categories', 'purpose', 'prompt', 'style_refs', 'texts', 'photo_codes', 'product_mode', 'product_box', 'gen_size', 'export', 'count', 'wear', 'enabled', 'lock_shape', 'size_compare', 'hero_style'] as const
export const templatePatch = (from: CreativeTemplate, to: CreativeTemplate) => {
  const out: Raw = {}
  for (const k of TEMPLATE_FIELDS) if (JSON.stringify(from[k]) !== JSON.stringify(to[k])) out[k] = k === 'product_box' ? (to[k] || []) : to[k]
  return out as Partial<CreativeTemplate>
}

// ---------- 接口（mock 开着时走假数据） ----------
export const mockOn = () => { try { return localStorage.getItem('xs-mock') === '1' } catch { return false } }
const wait = <T,>(v: T) => new Promise<T>(r => setTimeout(() => r(structuredClone(v)), 200))

export const creativeApi = {
  sets: (listingId: number) => mockOn() ? wait({ items: MOCK_SETS(listingId), total: 1 }) : api<Page<Raw>>(`/api/listings/${listingId}/creative`).then(page(normSet)),
  set: (id: number) => mockOn() ? wait(MOCK_SETS(1)[0]) : api<Raw>(`/api/creative/sets/${id}`).then(normSet),
  create: (listingId: number, body: { parts: string[]; params: SetParams }) => mockOn() ? wait({ set: MOCK_SETS(listingId)[0], jobs: [] }) : api<{ set: Raw; jobs: unknown[] }>(`/api/listings/${listingId}/creative`, { body }).then(r => ({ ...r, set: normSet(r.set) })),
  cont: (id: number) => mockOn() ? wait({}) : api(`/api/creative/sets/${id}/continue`, { method: 'POST' }),
  glance: (id: number) => mockOn() ? wait({}) : api(`/api/creative/sets/${id}/glance`, { method: 'POST' }),
  exportSet: (id: number) => mockOn() ? wait({ file: '' }) : api<{ file: string }>(`/api/creative/sets/${id}/export`, { method: 'POST' }),
  redraw: (id: number, note: string) => mockOn() ? wait({}) : api(`/api/creative/items/${id}/redraw`, { body: { note } }),
  retext: (id: number, key: string, text: string) => mockOn() ? wait({}) : api(`/api/creative/items/${id}/retext`, { body: { key, text } }),
  choose: (id: number) => mockOn() ? wait({}) : api(`/api/creative/items/${id}/choose`, { body: { variant: 'A' } }),
  judge: (id: number, verdict: Verdict, note: string) => mockOn() ? wait({}) : api(`/api/creative/items/${id}/judge`, { body: { verdict, note } }),
  review: (p: { shop_id?: number; slot?: string; page?: number }) => mockOn() ? wait({ items: MOCK_REVIEW, total: MOCK_REVIEW.length }) : api<Page<Raw>>('/api/creative/review' + qs(p)).then(page(normItem)),
  templates: (p: { kind?: string; category?: string }) => mockOn() ? wait({ items: MOCK_TEMPLATES, total: MOCK_TEMPLATES.length }) : api<Page<Raw>>('/api/creative/templates' + qs(p)).then(page(normTemplate)),
  saveTemplate: (key: string, body: Partial<CreativeTemplate>) => mockOn() ? wait({}) : api(`/api/creative/templates/${key}`, { method: 'PATCH', body }),
  trial: (key: string, listingId: number) => mockOn() ? wait({ item: MOCK_REVIEW[0] }) : api<{ item?: Raw; job_id?: number }>(`/api/creative/templates/${key}/trial`, { body: { listing_id: listingId } }).then(r => ({ ...r, item: r.item && normItem(r.item) })),
  shop: (id: number) => mockOn() ? wait<ShopCreative>({ models: { female_main: null, male: null, hand: null }, template_set: {} }) : api<Raw>(`/api/creative/shops/${id}/settings`).then(r => ({ models: r.models || {}, template_set: r.template_set || {} }) as ShopCreative),
  saveShop: (id: number, body: ShopCreative) => mockOn() ? wait({}) : api(`/api/creative/shops/${id}/settings`, { method: 'PUT', body }),
  stats: () => mockOn() ? wait(MOCK_STATS) : api<Stats>('/api/creative/stats'),
  suggestion: (id: number, status: 'done' | 'ignored') => mockOn() ? wait({}) : api(`/api/creative/suggestions/${id}`, { body: { status } }),
  calibration: (category: string, body: { tightened?: false; open?: false }) => mockOn() ? wait({}) : api(`/api/creative/calibration/${encodeURIComponent(category)}`, { method: 'PATCH', body })
}

// ---------- 假数据 ----------
const PH = (label: string, bg = '3A3F47') => `data:image/svg+xml;utf8,${encodeURIComponent(`<svg xmlns="http://www.w3.org/2000/svg" width="300" height="400"><rect width="300" height="400" fill="#${bg}"/><circle cx="150" cy="230" r="34" fill="#E8E6E1"/><text x="150" y="60" fill="#C9CDD3" font-size="20" text-anchor="middle" font-family="sans-serif">${label}</text></svg>`)}`
const rv = (score: number, product: Check = 'pass', issues: Review['issues'] = []): Review => ({ checks: { product, size: 'na', body: 'na', placement: 'na', style: 'pass' }, size_mm: [], score, issues })
const mk = (id: number, slot: string, over: Partial<CreativeItem> = {}): CreativeItem => ({
  id, set_id: 1, slot, seq: 1, template_key: 'tpl_' + slot, template_version: 1, attempt: 1, superseded: false, variant: '',
  product_mode: 'pixel_lock', source_asset_ids: [11], model_id: null, wear_type: '', size_compare: false, prompt: '…', texts: [],
  output_asset_id: 100 + id, output_url: PH(slotName(slot)), source_urls: [PH('原图', '8A8F96')],
  hard: { pixel: { pass: true, max_diff: 1 }, format: { pass: true }, text: [], banned: [] },
  review_a: rv(94), review_b: rv(92), route: 'auto_pass', route_reasons: [], risk: 0, human: '', human_note: '', error: '', created_at: new Date().toISOString(), ...over
})
const MOCK_ITEMS: CreativeItem[] = [
  mk(1, 'main1', { variant: 'A', route: 'manual', route_reasons: ['首图强制人工'], risk: 86 }),
  mk(2, 'main1', { variant: 'B', route: 'manual', route_reasons: ['首图强制人工'], risk: 88, review_b: rv(78, 'unsure', [{ check: 'product', box: [0.35, 0.5, 0.3, 0.2], reason: '右侧耳钉的银针看不清' }]) }),
  mk(3, 'main2', { product_mode: 'model', model_id: 3, wear_type: 'earring', route: 'manual', route_reasons: ['校准期'], risk: 72,
    review_a: { checks: { product: 'pass', size: 'pass', body: 'unsure', placement: 'pass', style: 'pass' }, size_mm: [8.4], score: 82, issues: [{ check: 'body', box: [0.55, 0.3, 0.2, 0.2], reason: '耳针和耳垂连接处有点虚' }] } }),
  mk(4, 'main3', { product_mode: 'photo' }),
  mk(5, 'main4', { product_mode: 'model', wear_type: 'earring', size_compare: true, seq: 1, route: 'manual', route_reasons: ['多尺寸对比图'], risk: 80, texts: [{ key: 'mm', text: '8–9 mm', critical: true }],
    hard: { format: { pass: true }, text: [{ key: 'mm', expect: '8–9 mm', found: '8–9 nm', pass: false, critical: true }] } }),
  mk(6, 'main4', { product_mode: 'model', wear_type: 'earring', size_compare: true, seq: 2, route: 'manual', route_reasons: ['多尺寸对比图'], risk: 80, texts: [{ key: 'mm', text: '10–11 mm', critical: true }] }),
  mk(7, 'main5', { attempt: 2 }),
  mk(8, 'main5', { superseded: true, route: 'auto_redraw', review_a: rv(41, 'fail', [{ check: 'product', box: [0.2, 0.3, 0.6, 0.3], reason: '粉色那对变成了紫色' }]), review_b: rv(45, 'fail') }),
  mk(9, 'd1', { texts: [{ key: 'name', text: '初雪', critical: false }], hard: { format: { pass: true }, text: [{ key: 'name', expect: '初雪', found: '初雪', pass: true }] } }),
  mk(10, 'd2', { error: '图像服务返回的尺寸不对' }),
  mk(11, 'd5', { route: 'manual', route_reasons: ['抽检'], risk: 22, texts: [{ key: 'params', text: '珠径 约 8–9 mm', critical: true }] }),
  mk(12, 'd9', { product_mode: 'none', human: 'pass', human_note: '' })
]
const MOCK_SETS = (listingId: number): CreativeSet[] => [{
  id: 1, listing_id: listingId, version: 1, status: 'reviewing', template_keys: {}, params: { hero_style: 'vertical', hero_b: true, sizes: [{ label: '8–9 mm', mm: 8.5 }, { label: '10–11 mm', mm: 10.5 }] },
  hero_b_item_id: 2, images_used: 17, images_allowance: 40, glanced_by: null, glanced_at: '', exported_at: '', export_file: '',
  created_by: 1, created_at: new Date(Date.now() - 3600_000).toISOString(), updated_at: new Date().toISOString(), items: MOCK_ITEMS,
  jobs: [{ id: 9, kind: 'creative.detail', status: 'running', progress: 55, message: '第 6 屏生成中' }]
}]
const MOCK_REVIEW = MOCK_ITEMS.filter(i => i.route === 'manual' && !i.human && !i.superseded).sort((a, b) => b.risk - a.risk).map(i => ({ ...i, listing_id: 1, product_id: 1, product_name: '强光爱迪生蝴蝶结耳钉', shop_id: 1 }))
const MOCK_TEMPLATES: CreativeTemplate[] = [
  { key: 'main_hero_stone', name: '首图·中灰石材', kind: 'main', slot: 'main1', categories: [], purpose: '首图 A/B，商品居中偏下，画面不加字', prompt: 'Hero image. The exact product ({商品}) lies on a matte mid-grey stone surface…', style_refs: [], texts: [], photo_codes: ['A1', 'A2', 'E4', 'B2'], product_mode: 'pixel_lock', product_box: [0.22, 0.34, 0.56, 0.36], gen_size: '1024x1536', export: [{ name: '3x4', w: 1440, h: 1920 }, { name: '1x1', w: 1440, h: 1440 }], count: 1, wear: null, version: 1, enabled: true, source: 'file' },
  { key: 'main_wear', name: '主图2·上身', kind: 'main', slot: 'main2', categories: [], purpose: '上身效果', prompt: 'On-body image…', style_refs: [], texts: [], photo_codes: ['A1', 'A2'], product_mode: 'model', product_box: null, gen_size: '1024x1536', export: [{ name: '3x4', w: 1440, h: 1920 }], count: 1, wear: { slot: 'female_main', body_part: 'auto', framing: 'half_body' }, version: 2, enabled: true, source: 'custom' },
  { key: 'detail_size', name: '第5屏·尺寸参数', kind: 'detail', slot: 'd5', categories: [], purpose: '刻度尺实拍加参数表', prompt: 'Size and specification screen…', style_refs: [], texts: [{ key: 'params', from: 'content.params', max: 120, critical: true }], photo_codes: ['C1', 'C2'], product_mode: 'pixel_lock', product_box: [0.06, 0.04, 0.88, 0.44], gen_size: '1024x1536', export: [{ name: '750', w: 750, h: 0 }], count: 1, wear: null, version: 1, enabled: true, source: 'file' },
  { key: 'detail_series', name: '第6屏·同系列', kind: 'detail', slot: 'd6', categories: [], purpose: '搭配推荐', prompt: 'Series screen…', style_refs: [], texts: [], photo_codes: ['A1'], product_mode: 'pixel_lock', product_box: [0.15, 0.4, 0.4, 0.3], gen_size: '1024x1536', export: [{ name: '750', w: 750, h: 0 }], count: 1, wear: null, version: 1, enabled: false, source: 'file' }
]
const MOCK_STATS: Stats = {
  categories: [
    { category: 'earring', total: 14, agree: 13, open: false, tightened: false, auto_pass_rate: 0, override_rate: 0.07, return_links: 0 },
    { category: 'necklace', total: 26, agree: 25, open: true, tightened: false, auto_pass_rate: 0.62, override_rate: 0.04, return_links: 1 },
    { category: 'tight:main4', total: 0, agree: 0, open: false, tightened: true, tightened_reason: '「比想象小」退货 2 次', return_links: 2 }
  ],
  suggestions: [{ id: 1, listing_id: 1, set_id: 1, item_id: 5, slot: 'main4', reason_code: 'smaller', count: 2, status: 'open', product_name: '强光爱迪生蝴蝶结耳钉', created_at: new Date().toISOString() }]
}
