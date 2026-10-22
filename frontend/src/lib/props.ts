import { api } from './api'
import type { Job } from './types'

// 道具库：道具设定卡（名称、种类、外观、真实尺寸）+ 锁定的参考图（正面 + 补的角度）
export type PropView = 'main' | 'left' | 'right' | 'top' | 'back' | 'other'
export type PropRef = { id: number; view: PropView; source: 'upload' | 'ai'; status: 'candidate' | 'locked' | 'discarded'; url: string; width: number; height: number; prompt?: string; model?: string; created_at: string }
export type SizeCM = { w?: number; d?: number; h?: number }
export type Prop = {
  id: number; shop_id: number | null; name: string; kind: string; description: string; size_cm: SizeCM
  cover_ref_id: number | null; cover_url: string; locked_views: Partial<Record<PropView, number>>; archived: boolean
  refs?: PropRef[]; created_at: string; updated_at: string
}
export type PropInput = Pick<Prop, 'name' | 'kind' | 'description' | 'size_cm' | 'shop_id'>

export const PROP_KINDS: Record<string, string> = { table: '桌面台面', container: '盒子托盘', fabric: '布料', plant: '花植', backdrop: '背景板', ornament: '小摆件', other: '其他' }
export const PROP_VIEWS: Record<PropView, string> = { main: '正面', left: '左侧', right: '右侧', top: '正上方', back: '背面', other: '另一角度' }
export const ANGLES: PropView[] = ['left', 'right', 'top', 'back', 'other']

export const sizeText = (s: SizeCM) => [s.w && `宽 ${s.w}`, s.d && `深 ${s.d}`, s.h && `高 ${s.h}`].filter(Boolean).join(' × ') + (s.w || s.d || s.h ? ' cm' : '')

const norm = (p: Prop): Prop => ({ ...p, size_cm: p.size_cm || {}, locked_views: p.locked_views || {}, refs: p.refs || [] })

export const propApi = {
  list: (p: { archived?: boolean; kind?: string; q?: string } = {}) => {
    const qs = new URLSearchParams({ archived: p.archived ? '1' : '0' })
    if (p.kind) qs.set('kind', p.kind)
    if (p.q) qs.set('q', p.q)
    return api<{ items: Prop[] }>('/api/props?' + qs).then(r => (r.items || []).map(norm))
  },
  get: (id: number) => api<{ prop: Prop }>(`/api/props/${id}`).then(r => norm(r.prop)),
  create: (body: PropInput) => api<{ prop: Prop }>('/api/props', { body }).then(r => norm(r.prop)),
  patch: (id: number, body: Partial<PropInput> & { archived?: boolean }) => api<{ prop: Prop }>(`/api/props/${id}`, { method: 'PATCH', body }).then(r => norm(r.prop)),
  // 被场景用着时只归档（archived=true），否则真删
  remove: (id: number) => api<{ archived?: boolean; deleted?: boolean }>(`/api/props/${id}`, { method: 'DELETE' }),
  upload: (id: number, file: File, view: PropView, lock: boolean) => { const form = new FormData(); form.append('file', file); form.append('view', view); if (lock) form.append('lock', 'true'); return api(`/api/props/${id}/refs`, { form }) },
  lock: (id: number, ref: number) => api<{ prop: Prop }>(`/api/props/${id}/refs/${ref}/lock`, { method: 'POST' }).then(r => norm(r.prop)),
  discard: (id: number, ref: number) => api<{ prop: Prop }>(`/api/props/${id}/refs/${ref}/discard`, { method: 'POST' }).then(r => norm(r.prop)),
  generate: (id: number, count: number, extra: string) => api<{ job_id: number; job: Job }>(`/api/props/${id}/generate`, { body: { count, extra } }),
  views: (id: number, views: PropView[]) => api<{ job_id: number; job: Job }>(`/api/props/${id}/views`, { body: { views } })
}
