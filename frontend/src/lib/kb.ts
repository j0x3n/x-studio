// 知识库：类型、关联对象、版本对比
export type KbFolder = { id: number; name: string; parent_id: number | null; sort: number; article_count: number; children?: KbFolder[] }
// 目录接口返回树（一级目录带 children），页面里按平铺列表用
export function flattenFolders(r: unknown): KbFolder[] {
  const list = Array.isArray(r) ? r as KbFolder[] : ((r as { items?: KbFolder[] })?.items || [])
  const out: KbFolder[] = []
  const walk = (fs: KbFolder[]) => fs.forEach(f => { out.push({ ...f, children: undefined }); if (f.children?.length) walk(f.children) })
  walk(list)
  return out
}
export type KbLinkType = 'category' | 'product' | 'item'
export type KbLink = { type: KbLinkType; id: number; label: string; path: string }
export type KbPerson = { id: number; name: string } | null
export type KbArticle = {
  id: number; folder: { id: number; name: string }; title: string; summary: string; tags: string[]
  pinned: boolean; version: number; links: KbLink[]; updated_by: KbPerson; updated_at: string
  created_by?: number; created_at?: string
}
export type KbVersionMeta = { version: number; user: KbPerson; note: string; created_at: string }
export type KbFile = { id: number; url: string; name: string; mime: string; size?: number; width?: number; height?: number }
export type KbDetail = KbArticle & { body: string; versions: KbVersionMeta[]; files: KbFile[] }
export type KbVersion = { version: number; title: string; body: string; folder_id: number; tags: string[]; note: string; user?: KbPerson; created_at: string }
export type KbDraft = { title: string; body: string; summary: string; tags: string[]; folder_id: number }

export const linkTypeName: Record<KbLinkType, string> = { category: '品类', product: '商品', item: '物料' }

// 后端可能把 versions/files 平铺在详情里，也可能是 {article, …}；统一成 KbDetail
export function normDetail(r: unknown): KbDetail {
  const o = (r && typeof r === 'object' ? r : {}) as Record<string, unknown>
  const a = (o.article && typeof o.article === 'object' ? { ...(o.article as object), ...o } : o) as unknown as KbDetail
  return { ...a, tags: a.tags || [], links: a.links || [], versions: a.versions || [], files: a.files || [] }
}

// 按行对比两个版本（最长公共子序列），给版本对比用
export type DiffLine = { kind: 'same' | 'add' | 'del'; text: string }
export function diffLines(a: string, b: string): DiffLine[] {
  const x = (a || '').replace(/\r\n?/g, '\n').split('\n')
  const y = (b || '').replace(/\r\n?/g, '\n').split('\n')
  // 太长时退化成整段替换，避免卡住页面
  if (x.length * y.length > 4_000_000) return [...x.map(t => ({ kind: 'del' as const, text: t })), ...y.map(t => ({ kind: 'add' as const, text: t }))]
  const n = x.length, m = y.length
  const dp: Uint32Array[] = Array.from({ length: n + 1 }, () => new Uint32Array(m + 1))
  for (let i = n - 1; i >= 0; i--) for (let j = m - 1; j >= 0; j--) dp[i][j] = x[i] === y[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1])
  const out: DiffLine[] = []
  let i = 0, j = 0
  while (i < n && j < m) {
    if (x[i] === y[j]) { out.push({ kind: 'same', text: x[i] }); i++; j++ }
    else if (dp[i + 1][j] >= dp[i][j + 1]) out.push({ kind: 'del', text: x[i++] })
    else out.push({ kind: 'add', text: y[j++] })
  }
  while (i < n) out.push({ kind: 'del', text: x[i++] })
  while (j < m) out.push({ kind: 'add', text: y[j++] })
  return out
}

export const fmtSize = (n?: number) => (!n ? '' : n < 1024 * 1024 ? `${Math.round(n / 1024)} KB` : `${(n / 1024 / 1024).toFixed(1)} MB`)
