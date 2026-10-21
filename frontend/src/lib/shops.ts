import type { Shop } from './types'

// 店铺和渠道（接口约定-素材场景店铺.md 第 3 节）：同一平台可以开多个店，直播间也算一个渠道
export const PLATFORMS: { key: string; name: string; sites?: boolean; kind: 'shop' | 'live' | 'other' }[] = [
  { key: 'taobao', name: '淘宝', kind: 'shop' },
  { key: 'tmall', name: '天猫', kind: 'shop' },
  { key: 'pdd', name: '拼多多', kind: 'shop' },
  { key: 'douyin', name: '抖音小店', kind: 'shop' },
  { key: 'xiaohongshu', name: '小红书', kind: 'shop' },
  { key: 'shopee', name: '虾皮', sites: true, kind: 'shop' },
  { key: 'lazada', name: 'Lazada', sites: true, kind: 'shop' },
  { key: 'tiktok', name: 'TikTok Shop', sites: true, kind: 'shop' },
  { key: 'website', name: '独立站', kind: 'shop' },
  { key: 'live', name: '直播间', kind: 'live' },
  { key: 'x', name: 'X', kind: 'other' },
  { key: 'other', name: '其他', kind: 'other' }
]
export const SITES: Record<string, string> = { tw: '台湾', my: '马来西亚', sg: '新加坡', th: '泰国', ph: '菲律宾', vn: '越南', id: '印尼', br: '巴西' }
export const SITE_CURRENCY: Record<string, string> = { tw: 'TWD', my: 'MYR', sg: 'SGD', th: 'THB', ph: 'PHP', vn: 'VND', id: 'IDR', br: 'BRL' }
export const CURRENCIES = ['CNY', 'TWD', 'MYR', 'SGD', 'THB', 'PHP', 'VND', 'IDR', 'BRL', 'USD']
export const LANGUAGES: Record<string, string> = { 'zh-CN': '简体中文', 'zh-TW': '繁体中文', en: '英文' }

export const platformOf = (key: string) => PLATFORMS.find(p => p.key === key)
// 旧数据 shopee_tw 也认（后端迁移后是 shopee + tw）
export function shopLabel(s: Pick<Shop, 'platform'> & { site?: string }) {
  if (s.platform === 'shopee_tw') return '虾皮 · 台湾'
  const p = platformOf(s.platform)
  return (p?.name || s.platform) + (s.site ? ` · ${SITES[s.site] || s.site}` : '')
}
// 预览商品页用哪种版式
export const previewStyle = (s?: Pick<Shop, 'platform'>) => !s ? 'taobao' : ['shopee', 'shopee_tw', 'lazada', 'tiktok'].includes(s.platform) ? 'shopee' : 'taobao'
