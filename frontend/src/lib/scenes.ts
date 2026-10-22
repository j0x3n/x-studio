import { api } from './api'
import type { Job } from './types'

// 场景库：静物台面 + 上身穿搭 + 光线色调 + 道具 + 参考图
export type SceneRef = { id: number; use: 'still' | 'wear'; url: string; width: number; height: number }
// 布局：画布上的商品框（洋红）和道具框，box 是 [x, y, w, h]，都是 0–1 的比例
export type PlateSize = '1024x1536' | '1024x1024' | '1536x1024'
export type LayoutElement = { id: string; type: 'product' | 'prop'; prop_id?: number; label: string; box: [number, number, number, number]; note: string; prop_name?: string; cover_url?: string }
export type Layout = { size: PlateSize; elements: LayoutElement[] }
// 定场图：按布局画好的空台面（商品框留空），锁定后出图时把商品放进商品框
export type Plate = { id: number; status: 'candidate' | 'locked' | 'discarded'; url: string; sketch_url?: string; width: number; height: number; size: string; layout_snapshot: Layout; created_at: string }
export type Scene = {
  id: number; shop_id: number | null; name: string; categories: string[]
  still: string; wear: string; light: string; props: string; note: string
  camera: Camera; brief: string; layout: Layout
  plate: Plate | null; plates: Plate[]; plate_outdated: boolean; thumb_url: string
  builtin_key: string | null; is_default: boolean; archived: boolean
  refs: SceneRef[]; usage: { listings: number; shops: number }
  created_at: string; updated_at: string
}
export type SceneInput = Pick<Scene, 'name' | 'categories' | 'still' | 'wear' | 'light' | 'props' | 'note'> & { shop_id?: number | null; camera?: Camera; brief?: string; layout?: Layout }
export type Camera = '' | 'top' | 'angle45' | 'eye' | 'macro'
export const CAMERAS: Record<Camera, string> = { '': '不指定', top: '顶拍', angle45: '俯拍 45°', eye: '平视', macro: '微距特写' }
export const PLATE_SIZES: Record<PlateSize, string> = { '1024x1536': '竖 2:3', '1024x1024': '方 1:1', '1536x1024': '横 3:2' }
// 和后端 layout.go 的颜色顺序一致：商品框洋红，道具按出现顺序轮流用这 8 色
export const PRODUCT_COLOR = '#FF00FF'
export const PROP_COLORS = [['红', '#DC2828'], ['蓝', '#285ADC'], ['绿', '#1EA046'], ['橙', '#E67814'], ['紫', '#8C32BE'], ['青', '#14A0B4'], ['棕', '#8C5A28'], ['灰', '#5A5A5A']] as const
export type Binding = { listing_id: number; scene_id: number | null; source: 'listing' | 'shop' | 'default' | 'none'; scene_name: string }
export type ShopBinding = { shop_id: number; scene_id: number | null; scene_name: string }

const norm = (s: Scene): Scene => ({ ...s, categories: s.categories || [], refs: s.refs || [], usage: s.usage || { listings: 0, shops: 0 }, camera: s.camera || '', brief: s.brief || '', layout: { size: s.layout?.size || '1024x1536', elements: s.layout?.elements || [] }, plate: s.plate || null, plates: s.plates || [], plate_outdated: !!s.plate_outdated, thumb_url: s.thumb_url || '' })

export const sceneApi = {
  list: (p: { shop_id?: number; category?: string; archived?: boolean; q?: string } = {}) => {
    const qs = new URLSearchParams()
    if (p.shop_id) qs.set('shop_id', String(p.shop_id))
    if (p.category) qs.set('category', p.category)
    if (p.q) qs.set('q', p.q)
    qs.set('archived', p.archived ? '1' : '0')
    return api<{ items: Scene[] }>('/api/scenes?' + qs).then(r => (r.items || []).map(norm))
  },
  get: (id: number) => api<{ scene: Scene }>(`/api/scenes/${id}`).then(r => norm(r.scene)),
  create: (body: SceneInput) => api<{ scene: Scene }>('/api/scenes', { body }).then(r => norm(r.scene)),
  patch: (id: number, body: Partial<SceneInput> & { archived?: boolean }) => api<{ scene: Scene }>(`/api/scenes/${id}`, { method: 'PATCH', body }).then(r => norm(r.scene)),
  remove: (id: number) => api(`/api/scenes/${id}`, { method: 'DELETE' }),
  addRef: (id: number, file: File, use: 'still' | 'wear') => { const form = new FormData(); form.append('file', file); form.append('use', use); return api(`/api/scenes/${id}/refs`, { form }) },
  removeRef: (id: number, ref: number) => api(`/api/scenes/${id}/refs/${ref}`, { method: 'DELETE' }),
  bindings: (listingIds: number[]) => api<{ listings: Binding[]; shops: ShopBinding[] }>('/api/scenes/bindings?listing_ids=' + listingIds.join(',')).then(r => ({ listings: r.listings || [], shops: r.shops || [] })),
  // 店铺默认场景（bindings?shop_ids=）
  shopBinding: (shopId: number) => api<{ shops: ShopBinding[] }>('/api/scenes/bindings?shop_ids=' + shopId).then(r => (r.shops || []).find(x => x.shop_id === shopId)?.scene_id || null),
  plate: (id: number, extra: string) => api<{ job_id: number; job: Job }>(`/api/scenes/${id}/plates`, { body: { extra } }),
  lockPlate: (id: number, plate: number) => api(`/api/scenes/${id}/plates/${plate}/lock`, { method: 'POST' }),
  discardPlate: (id: number, plate: number) => api(`/api/scenes/${id}/plates/${plate}/discard`, { method: 'POST' }),
  bind: (body: { listing_ids: number[]; scene_id: number | null } | { shop_id: number; scene_id: number | null }) => api('/api/scenes/bindings', { method: 'PUT', body })
}

export const SCENE_SOURCE: Record<Binding['source'], string> = { listing: '单独指定', shop: '店铺默认', default: '默认场景', none: '没有场景' }
