// 商品底图槽位、AI 分槽位、高清化替换、基于商品新建、出图方案（接口说明-素材和方案.md）
import { api, qs, type Page } from './api'
import type { Asset, Job, Product } from './types'

// 底图接口里的照片多带这几项；suggest、enhance_check 缺省是 {}
export type Suggest = { code?: string; confidence?: number; reason?: string }
export type EnhanceCheck = { blur_before?: number; blur_after?: number; fidelity?: number; issues?: string[]; verdict?: 'pass' | 'fail'; pending?: boolean }
export type ViewAsset = Asset & { view_code?: string; blur_level?: string; suggest?: Suggest; enhance_check?: EnhanceCheck }
export type ViewSlot = { key: string; name: string; code: string; required: boolean; hint: string; current: ViewAsset | null; versions: ViewAsset[]; pending: ViewAsset[] }
export type ProductViews = { group: 'small' | 'strand'; views: ViewSlot[]; unassigned: ViewAsset[]; others: ViewAsset[]; missing: string[]; ready: boolean }

export const GROUP_NAME = { small: '小件（耳饰、戒指、吊坠）', strand: '链串（项链、手链、手串）' } as const

// 高清化候选还没确认
export const isPending = (a: ViewAsset) => !!a.enhance_check?.pending
export const hasSuggest = (a: ViewAsset) => !!a.suggest?.code || !!a.suggest?.reason
export const blurry = (a: ViewAsset) => a.blur_level === '模糊'

export const viewsApi = {
  get: (productId: number) => api<ProductViews>(`/api/products/${productId}/views`).then(r => ({ ...r, views: r.views || [], unassigned: r.unassigned || [], others: r.others || [], missing: r.missing || [] })),
  // code 为空表示不指定槽位
  upload: (productId: number, files: File[], code = '') => {
    const form = new FormData()
    if (code) form.append('code', code)
    files.forEach(f => form.append('files', f))
    return api(`/api/products/${productId}/assets`, { form })
  },
  setCode: (assetId: number, code: string) => api(`/api/assets/${assetId}`, { method: 'PATCH', body: { code } }),
  makeCurrent: (assetId: number) => api<{ asset: Asset }>(`/api/assets/${assetId}/make-current`, { method: 'POST' }),
  discard: (assetId: number) => api<{ asset: Asset }>(`/api/assets/${assetId}/discard`, { method: 'POST' }),
  classify: (productId: number, assetIds?: number[]) => api<{ job_id: number; job: Job }>(`/api/products/${productId}/views/classify`, { body: assetIds?.length ? { asset_ids: assetIds } : {} }),
  enhance: (productId: number, assetIds: number[], replace: 'confirm' | 'auto') => api<{ job_id: number; job: Job }>(`/api/products/${productId}/views/enhance`, { body: { asset_ids: assetIds, replace } }),
  duplicate: (productId: number, name: string) => api<{ product: Product }>(`/api/products/${productId}/duplicate`, { body: { name } })
}

// ---------- 出图方案 ----------
export type ModelSlotKey = 'female_main' | 'male' | 'hand'
export type PlanConfig = { template_set: Record<string, string>; models: Partial<Record<ModelSlotKey, number[]>>; scene_id: number | null; detail_layout: string }
export type Plan = PlanConfig & { id: number; name: string; note: string; categories: string[]; archived: boolean; created_by: number; created_at: string; updated_at: string }
export type PlanInput = Partial<Pick<Plan, 'name' | 'note' | 'categories' | 'template_set' | 'models' | 'scene_id' | 'detail_layout' | 'archived'>>
// 商品覆盖：只写想改的位置；字段缺省 = 沿用方案
export type PlanOverrides = Partial<PlanConfig>
export type ProductPlan = { plan: Plan | null; overrides: PlanOverrides; effective: PlanConfig }

export const MODEL_SLOTS: Record<ModelSlotKey, string> = { female_main: '女主模特', male: '男模特', hand: '手部模特' }
const normPlan = (p: Plan): Plan => ({ ...p, categories: p.categories || [], template_set: p.template_set || {}, models: p.models || {}, scene_id: p.scene_id ?? null, detail_layout: p.detail_layout || '' })

export const planApi = {
  list: (archived = false) => api<Page<Plan>>('/api/creative/plans' + qs({ archived: archived ? 1 : undefined, page_size: 200 })).then(r => (r.items || []).map(normPlan)),
  get: (id: number) => api<{ plan: Plan }>(`/api/creative/plans/${id}`).then(r => normPlan(r.plan)),
  create: (body: PlanInput) => api<{ plan: Plan }>('/api/creative/plans', { body }).then(r => normPlan(r.plan)),
  patch: (id: number, body: PlanInput) => api<{ plan: Plan }>(`/api/creative/plans/${id}`, { method: 'PATCH', body }).then(r => normPlan(r.plan)),
  remove: (id: number) => api<{ archived: boolean; deleted: boolean }>(`/api/creative/plans/${id}`, { method: 'DELETE' }),
  ofProduct: (productId: number) => api<ProductPlan>(`/api/products/${productId}/plan`).then(r => ({ plan: r.plan ? normPlan(r.plan) : null, overrides: r.overrides || {}, effective: { template_set: r.effective?.template_set || {}, models: r.effective?.models || {}, scene_id: r.effective?.scene_id ?? null, detail_layout: r.effective?.detail_layout || '' } })),
  setProduct: (productId: number, body: { plan_id?: number | null; plan_overrides?: PlanOverrides }) => api(`/api/products/${productId}`, { method: 'PATCH', body })
}
