import type { Asset, AssetOp, WbItem } from './types'
import { assetTitle, imgUrl } from './format'

export const BODY_PARTS = { full_body: '全身', upper_body: '上半身', hand: '手部', wrist: '手腕', ear: '耳部', neck: '颈部', foot: '脚部', custom: '自定义' } as const
export type BodyPart = keyof typeof BODY_PARTS
export const FRAMINGS = { full_body: '全身照', half_body: '半身照', closeup: '局部特写' } as const
export type Framing = keyof typeof FRAMINGS
export const ASPECTS = { '1:1': '方图 1:1', '2:3': '竖图 2:3', '3:2': '横图 3:2' } as const
export type Aspect = keyof typeof ASPECTS
export type ModelReference = { id: number; model_id: number; body_part: BodyPart; source: string; orig_name: string; width: number; height: number; url: string; thumb_url: string; op?: Record<string, unknown>; created_at: string }
export type ShopModel = { id: number; shop_id: number; name: string; description: string; archived: boolean; cover_reference_id: number | null; cover_url: string; references: ModelReference[]; created_by: number; created_at: string; updated_at: string }
export type TryOnOp = AssetOp & { type?: 'tryon' | 'model_candidate'; model_id?: number; shop_id?: number; model_name?: string; reference_id?: number; body_part?: BodyPart; framing?: Framing; product_type?: string; prompt?: string; aspect_ratio?: Aspect; confirmed_reference_id?: number; inputs?: ImageInput[] }
export type ModelResult = Omit<WbItem, 'op'> & { op?: TryOnOp }
export type Analysis = { product_type: string; body_part: BodyPart; framing: Framing; description: string; suggested_prompt: string }
export type ImageInput = { asset_id: number; input_id?: never } | { input_id: number; asset_id?: never }
export type TryOnSource = { key: string; asset?: Asset; item?: WbItem }
export const sourceInput = (s: TryOnSource): ImageInput => s.asset ? { asset_id: s.asset.id } : { input_id: s.item!.id }
export const sourceLabel = (s: TryOnSource) => s.asset ? assetTitle(s.asset) : s.item!.orig_name
export const sourceImage = (s: TryOnSource) => s.asset ? imgUrl(s.asset, 480) : s.item!.url
export const bodyLabel = (v?: string) => BODY_PARTS[v as BodyPart] || v || '人物参考'
export const modelCover = (m: ShopModel) => m.cover_url || m.references?.find(r => r.id === m.cover_reference_id)?.thumb_url || m.references?.[0]?.thumb_url || ''
export const resultIds = (result?: Record<string, unknown> | null) => ((result?.items || []) as { result_id?: number }[]).map(i => i.result_id).filter((id): id is number => !!id)
export const expiresAt = (date: string) => new Date(new Date(date).getTime() + 7 * 86400_000).toLocaleDateString('zh-CN', { month: 'numeric', day: 'numeric' })
