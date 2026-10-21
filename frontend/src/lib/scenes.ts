import { api } from './api'

// 场景库（接口约定-素材场景店铺.md 第 2 节）：静物台面 + 上身穿搭 + 光线色调 + 道具 + 参考图
export type SceneRef = { id: number; use: 'still' | 'wear'; url: string; width: number; height: number }
export type Scene = {
  id: number; shop_id: number | null; name: string; categories: string[]
  still: string; wear: string; light: string; props: string; note: string
  builtin_key: string | null; is_default: boolean; archived: boolean
  refs: SceneRef[]; usage: { listings: number; shops: number }
  created_at: string; updated_at: string
}
export type SceneInput = Pick<Scene, 'name' | 'categories' | 'still' | 'wear' | 'light' | 'props' | 'note'> & { shop_id?: number | null }
export type Binding = { listing_id: number; scene_id: number | null; source: 'listing' | 'shop' | 'default' | 'none'; scene_name: string }
export type ShopBinding = { shop_id: number; scene_id: number | null; scene_name: string }

const norm = (s: Scene): Scene => ({ ...s, categories: s.categories || [], refs: s.refs || [], usage: s.usage || { listings: 0, shops: 0 } })

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
  // 店铺默认场景（bindings?shop_ids=，接口约定 2.4 补充）
  shopBinding: (shopId: number) => api<{ shops: ShopBinding[] }>('/api/scenes/bindings?shop_ids=' + shopId).then(r => (r.shops || []).find(x => x.shop_id === shopId)?.scene_id || null),
  bind: (body: { listing_ids: number[]; scene_id: number | null } | { shop_id: number; scene_id: number | null }) => api('/api/scenes/bindings', { method: 'PUT', body })
}

export const SCENE_SOURCE: Record<Binding['source'], string> = { listing: '单独指定', shop: '店铺默认', default: '兜底场景', none: '没有场景' }
