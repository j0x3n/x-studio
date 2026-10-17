// 后端接口封装：同源 cookie 会话；写操作带 X-Requested-With: fetch（后端防 CSRF）；出错时抛出带中文说明的 ApiError。
export class ApiError extends Error {
  status: number
  constructor(message: string, status: number) {
    super(message)
    this.status = status
  }
}

type Options = { method?: string; body?: unknown; form?: FormData; signal?: AbortSignal }

export async function api<T = unknown>(path: string, opts: Options = {}): Promise<T> {
  const method = opts.method || (opts.body !== undefined || opts.form ? 'POST' : 'GET')
  const headers: Record<string, string> = { 'X-Requested-With': 'fetch' }
  let body: BodyInit | undefined
  if (opts.form) body = opts.form
  else if (opts.body !== undefined) {
    headers['Content-Type'] = 'application/json'
    body = JSON.stringify(opts.body)
  }
  const res = await fetch(path, { method, headers, body, credentials: 'same-origin', signal: opts.signal })
  if (res.status === 401 && !path.startsWith('/api/login')) {
    if (location.pathname !== '/login') location.assign('/login?next=' + encodeURIComponent(location.pathname + location.search))
    throw new ApiError('请先登录', 401)
  }
  const text = await res.text()
  let data: unknown = null
  if (text) {
    try { data = JSON.parse(text) } catch { data = null }
  }
  if (!res.ok) {
    const e = data && typeof data === 'object' && 'error' in data ? String((data as { error: unknown }).error) : ''
    const msg = e || `请求失败（${res.status}）`
    throw new ApiError(msg, res.status)
  }
  return data as T
}

export type Page<T> = { items: T[]; total: number }

export const qs = (o: Record<string, string | number | boolean | undefined | null>) => {
  const p = new URLSearchParams()
  for (const [k, v] of Object.entries(o)) if (v !== undefined && v !== null && v !== '' && v !== false) p.set(k, String(v))
  const s = p.toString()
  return s ? '?' + s : ''
}
