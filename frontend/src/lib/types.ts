// 后端返回的数据结构（只写前端用到的字段）。字段名和后端 JSON 一致。
export type Role = { id: number; key: string; name: string; builtin: boolean; scope: 'all' | 'shops' | 'shares'; perms: string[] }
export type Shop = { id: number; name: string; platform: 'taobao' | 'shopee_tw' | 'x' | 'other'; language: string; archived?: boolean; settings?: Record<string, unknown> }
export type Me = { user: { id: number; username: string; display_name: string }; role: Role; perms: string[]; shops: Shop[] }

export type Listing = {
  id: number; product_id: number; shop_id: number; status: 'draft' | 'ready' | 'listed'; url: string
  pipeline_status: string; pipeline_step: string; pipeline_error: string
  content?: Record<string, unknown>; main?: Record<string, unknown>; meta?: Record<string, unknown>
  updated_at: string
}
export type Product = {
  id: number; name: string; created_by: number; created_at: string; updated_at: string
  status: 'sampling' | 'pending' | 'listed' | 'archived'
  info: Record<string, unknown>; cost?: Record<string, unknown>; supplier?: Record<string, unknown>
  listings: Listing[]; cover_url: string; asset_count?: number
}
export type Asset = {
  id: number; product_id: number; file: string; orig_name: string; width: number; height: number
  code: string; source: string; source_override?: string; kind: string; tags: string[]; url: string
  ai_note: string; metrics: Record<string, unknown>; created_by: number; created_at: string; parent_id?: number | null
}
export type Job = { id: number; kind: string; status: 'queued' | 'running' | 'done' | 'failed'; progress: number; message: string; created_at: string; finished_at: string }
export type UserRow = { id: number; username: string; display_name: string; role_id: number; created_at: string; disabled: boolean; shop_ids?: number[]; quota?: { month_images: number | null; month_text: number | null } | null }
export type Permission = { key: string; name: string; group: string }
export type AuditRow = { id: number; at: string; user_id: number; action: string; target_type: string; target_id: number; detail: unknown; ip: string }
