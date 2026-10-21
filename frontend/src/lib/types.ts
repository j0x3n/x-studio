// 后端返回的数据结构（只写前端用到的字段）。字段名和后端 JSON 一致。
export type Role = { id: number; key: string; name: string; builtin: boolean; scope: 'all' | 'shops' | 'shares'; perms: string[] }
export type Shop = { id: number; name: string; platform: string; site?: string; currency?: string; language: string; archived?: boolean; settings?: Record<string, unknown> }
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
  category_id?: number; category?: { id: number; key: string; name: string }; created_by_name?: string
}
export type Asset = {
  id: number; product_id: number; file: string; orig_name: string; width: number; height: number
  code: string; source: string; source_override?: string; kind: string; tags: string[]; url: string
  ai_note: string; metrics: Record<string, unknown>; created_by: number; created_at: string; parent_id?: number | null
  // 图片库接口多出来的字段
  title?: string; note?: string; archived?: boolean; updated_at?: string
  product?: { id: number; name: string }; effective_source?: string; thumb_url?: string; version_count?: number
  op?: AssetOp; folder_id?: number | null
  // 视频和旧版本（接口约定-素材场景店铺.md 第 1 节）
  media?: 'image' | 'video'; duration_ms?: number; poster_url?: string; stale?: boolean
}
// 素材文件夹：团队共用的整理层级，一张素材最多在一个文件夹里；删文件夹不删图
export type AssetFolder = { id: number; parent_id: number | null; name: string; count: number; created_by: number; created_at: string }
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

// 文案中心
export type CopyVersion = {
  id: number; listing_id: number; title: string; source: string; note: string; created_by: number; created_at: string; applied_at: string
  content?: Record<string, unknown>; main?: Record<string, unknown>; meta?: Record<string, unknown>
}
export type Keyword = { id: number; shop_id: number | null; word: string; grp: string; weight: number; note: string; created_by: number; created_at: string }
// field 是 JSON 路径，如 content.oneLiner、content.details[0].text
export type Issue = { level: 'error' | 'warn'; field: string; text: string; reason: string; suggest: string }

// 模板中心和出图审核
export type DetailTemplate = { key: string; name: string; file: string; fit: string; optional: Record<string, string>; thumb_url: string; disabled: boolean; is_default: boolean; categories?: string[] }
export type Outputs = { main34: string[]; main11: string[]; detail: string[]; white: string; zip: string }
export type ReviewStatus = 'none' | 'pending' | 'approved' | 'rejected'
export type RenderState = {
  listing_id: number
  settings: { mode?: 'ai' | 'html'; detail_template: string; effective_detail_template: string; main_template: string; rendered_job_id: number | null; rendered_template: string; render_note: string; updated_at: string }
  review: { status: ReviewStatus; note: string; by: number | null; at: string }
  outputs: Outputs; preview_url: string
}
export type QueueItem = {
  listing_id: number; product_id: number; product_name: string; shop_id: number; shop_name: string
  detail_template: string; rendered_template: string; rendered_job_id: number | null; updated_at: string
  outputs: Outputs; preview_url: string
}

// 首页和 AI 助手（接口约定-AI助手.md）
export type AssistAction = { type: 'open' | 'run_listing' | 'render_listing' | 'render_batch' | 'process_assets' | 'generate_copy' | 'review' | 'create_task'; label: string; summary: string; params: Record<string, unknown>; cost_hint?: string; preview?: { assignee: string; due: string; target: string } }
export type AssistRef = { type: 'product' | 'listing' | 'asset' | 'job' | 'kb_article' | 'inv_item' | 'purchase_order'; id: number; label: string; path?: string }
export type AssistReply = { reply: string; actions: AssistAction[]; refs: AssistRef[]; dropped: number; rounds: number }
export type Todo = { kind: 'render_failed' | 'review_pending' | 'copy_unapplied' | 'no_photo'; level: 'danger' | 'warning' | 'info'; title: string; detail: string; count: number; product_id?: number; listing_id?: number; action: AssistAction | null }
export type Home = {
  counts: { in_progress: number; review_pending: number | null; failed: number; no_photo: number; copy_unapplied: number | null }
  todos: Todo[]
  recent_assets: { id: number; product_id: number; product_name: string; title: string; source: string; thumb_url: string; created_at: string }[]
}

// 多品类（接口约定-多品类.md）
export type AttrType = 'text' | 'textarea' | 'number' | 'select' | 'multi' | 'bool'
export type AttrField = { key: string; label: string; type: AttrType; options?: string[]; free?: boolean; unit?: string; hint?: string; required?: boolean }
export type ShotCode = { code: string; group: string; name: string; use?: string; desc?: string; suffix?: string }
export type Category = {
  id: number; key: string; name: string; sort: number; disabled: boolean
  attrs: AttrField[]; codes: ShotCode[]; slots: Record<string, unknown>; rules: string; banned: string[]; default_template: string
  product_count?: number; created_at: string; updated_at: string
}

// 任务和通知（接口约定-任务和通知.md）
export type TaskStatus = 'todo' | 'doing' | 'waiting' | 'done' | 'canceled'
export type TaskPriority = 'low' | 'normal' | 'high' | 'urgent'
export type PersonRef = { id: number; name: string }
export type Task = {
  id: number; title: string; body: string; status: TaskStatus; priority: TaskPriority
  assignee: PersonRef | null; created_by: PersonRef; due_at: string; overdue: boolean
  target: { type: string; id: number; label: string; path: string } | null
  checklist: { text: string; done: boolean }[]; checklist_done: number
  watchers: PersonRef[]; comment_count: number; rule_id: number | null; reopen_count: number
  created_at: string; updated_at: string; done_at: string
}
export type TaskComment = { id: number; task_id: number; user_id: number; user?: PersonRef; user_name?: string; body: string; mentions: number[]; created_at: string }
export type TaskEvent = { id: number; task_id: number; user_id: number; user?: PersonRef; user_name?: string; kind: string; detail: Record<string, unknown>; created_at: string }
export type Assignee = { id: number; name: string; role_name: string; open_count: number }
export type TaskRule = {
  id: number; name: string; enabled: boolean; event: string; title_tpl: string; body_tpl: string
  assign_mode: 'fixed' | 'role_round_robin' | 'role_least_loaded' | 'product_owner' | 'job_user'
  assign_user_id: number | null; assign_role_id: number | null; due_hours: number | null; priority: TaskPriority
}
export type Notice = { id: number; user_id: number; type: string; title: string; body: string; link: string; target_type: string; target_id: number; read_at: string; created_at: string; desktop?: boolean }
export type NoticePrefs = { types: Record<string, { inbox: boolean; desktop: boolean }>; quiet: { from: string; to: string } | null }
