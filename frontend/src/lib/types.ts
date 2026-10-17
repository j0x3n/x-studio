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
  // 图片库接口多出来的字段
  title?: string; note?: string; archived?: boolean; updated_at?: string
  product?: { id: number; name: string }; effective_source?: string; thumb_url?: string; version_count?: number
  op?: AssetOp
}
// 处理出来的新版本在 op 里记做了什么
export type AssetOp = {
  ops?: ProcessOps; preset_id?: number | null; prompts?: string[]; models?: Record<string, string>
  warnings?: string[]; diff?: { phash_distance: number; color_delta: number }; elapsed_ms?: number; cutout?: string
}
export type ProcessOps = {
  geometry?: { rotate: number; crop?: number[] }
  aspect?: '1:1' | '3:4' | '4:3' | '16:9' | 'free'
  color?: { auto_wb?: boolean; match_to?: number }
  cutout?: { background: 'white' | 'transparent' }
  upscale?: { target_long_side: number }
  ai?: { quick?: string[]; extra?: string }
  resize?: { long_side: number }
  format?: 'jpg' | 'png'
}
// 处理台里的图：上传 / 粘贴的原图（input）或处理结果（result），只有本人看得到，7 天后清掉
export type WbItem = {
  id: number; kind: 'input' | 'result'; orig_name: string; width: number; height: number; source: string
  asset_id: number | null; input_id: number | null; op?: AssetOp; saved_asset_id: number | null
  url: string; thumb_url: string; created_at: string
  asset?: { id: number; product_id: number; product_name: string; code: string }
}
export type Preset ={ id: number; name: string; owner_id: number; shared: boolean; params: { ops?: ProcessOps; set_current?: boolean }; updated_at: string }
export type Job = {
  id: number; kind: string; status: 'queued' | 'running' | 'done' | 'failed'; progress: number; message: string; created_at: string; finished_at: string
  target_type?: string; target_id?: number; result?: Record<string, unknown> | null
}
export type UserRow = { id: number; username: string; display_name: string; role_id: number; created_at: string; disabled: boolean; shop_ids?: number[]; quota?: { month_images: number | null; month_text: number | null } | null }
export type Permission = { key: string; name: string; group: string }
export type AuditRow = { id: number; at: string; user_id: number; action: string; target_type: string; target_id: number; detail: unknown; ip: string }
