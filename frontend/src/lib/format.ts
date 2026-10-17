// 显示用的格式化和中文标签
export const productStatus: Record<string, { label: string; pill: string }> = {
  sampling: { label: '拍样中', pill: 'pill-neutral' },
  pending: { label: '待上架', pill: 'pill-warning' },
  listed: { label: '已上架', pill: 'pill-success' },
  archived: { label: '已归档', pill: 'pill-neutral' }
}
export const listingStatus: Record<string, { label: string; pill: string }> = {
  draft: { label: '草稿', pill: 'pill-neutral' },
  ready: { label: '可上架', pill: 'pill-info' },
  listed: { label: '已上架', pill: 'pill-success' }
}
export const pipelineStatus: Record<string, { label: string; pill: string }> = {
  draft: { label: '未出图', pill: 'pill-neutral' },
  processing: { label: '出图中', pill: 'pill-info' },
  ready: { label: '已出图', pill: 'pill-success' },
  failed: { label: '失败', pill: 'pill-danger' }
}
export const platformName: Record<string, string> = { taobao: '淘宝', shopee_tw: '虾皮台湾', x: 'X', other: '其他' }
export const languageName: Record<string, string> = { 'zh-CN': '简体中文', 'zh-TW': '繁体中文', en: '英文' }
export const scopeName: Record<string, string> = { all: '全部数据', shops: '按分到的店铺', shares: '只看分享给他的款' }
export const sourceName: Record<string, { label: string; pill: string }> = {
  original: { label: '实拍', pill: 'pill-neutral' },
  ai_enhanced: { label: 'AI 增强', pill: 'pill-info' },
  ai_edited: { label: 'AI 改图', pill: 'pill-purple' },
  ai_generated: { label: 'AI 生成', pill: 'pill-warning' }
}

// 图片处理台的快捷需求，和 prompts/处理-快捷需求.md 的 key 一一对应。轻度项结果算「AI 增强」，改图项算「AI 改图」
export const quickOps: { key: string; label: string; heavy: boolean }[] = [
  { key: 'dust', label: '去灰尘', heavy: false },
  { key: 'glare', label: '去反光', heavy: false },
  { key: 'bright', label: '提亮', heavy: false },
  { key: 'wb', label: '校色', heavy: false },
  { key: 'bg_white', label: '纯白背景', heavy: true },
  { key: 'bg_linen', label: '亚麻布背景', heavy: true },
  { key: 'bg_velvet', label: '深色丝绒背景', heavy: true },
  { key: 'shadow', label: '加自然投影', heavy: true }
]
export const opName: Record<string, string> = { geometry: '旋转裁剪', aspect: '比例', color: '校色', cutout: '抠图', upscale: '高清化', ai: 'AI 处理', resize: '缩放', format: '格式' }
// 缩略图接口只认 240 / 480 / 960，且输出 JPEG；PNG（可能带透明）直接用原图
export const imgUrl = (a: { url: string; file?: string }, w: 240 | 480 | 960) => (/\.png$/i.test(a.file || '') ? a.url : `${a.url}?w=${w}`)
export const assetTitle = (a: { title?: string; orig_name: string; id: number }) => a.title || a.orig_name || `#${a.id}`

const pad = (n: number) => String(n).padStart(2, '0')
export function fmtDate(s?: string, withTime = false) {
  if (!s) return ''
  const d = new Date(s)
  if (isNaN(+d)) return s
  const base = `${d.getMonth() + 1}月${d.getDate()}日`
  const y = d.getFullYear() !== new Date().getFullYear() ? `${d.getFullYear()}年` : ''
  return y + base + (withTime ? ` ${pad(d.getHours())}:${pad(d.getMinutes())}` : '')
}
export function fmtAgo(s?: string) {
  if (!s) return ''
  const d = new Date(s)
  const sec = (Date.now() - +d) / 1000
  if (isNaN(sec)) return s
  if (sec < 60) return '刚刚'
  if (sec < 3600) return `${Math.floor(sec / 60)} 分钟前`
  if (sec < 86400) return `${Math.floor(sec / 3600)} 小时前`
  if (sec < 86400 * 7) return `${Math.floor(sec / 86400)} 天前`
  return fmtDate(s)
}
export const initials = (name: string) => (name || '?').trim().slice(0, 1).toUpperCase()
export const money = (n: unknown) => (typeof n === 'number' ? '¥' + n.toFixed(2) : '—')
