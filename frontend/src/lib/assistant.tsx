import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useLocation } from 'react-router-dom'
import { api } from './api'
import type { AssistAction, AssistReply, Job, Task } from './types'

// AI 助手：右侧面板的开关、对话记录、当前页面上下文。对话只存在本标签页（sessionStorage），后端不存。
export type ChatMsg = { role: 'user' | 'assistant'; content: string; actions?: AssistAction[]; refs?: AssistReply['refs']; error?: boolean }
export type Selection = { type: 'product' | 'listing' | 'asset' | 'kb_article' | 'inv_item' | 'purchase_order'; ids: number[] } | null

type Assistant = {
  open: boolean
  setOpen: (v: boolean) => void
  messages: ChatMsg[]
  busy: boolean
  send: (text: string) => void
  clear: () => void
  // 页面可以告诉助手当前选中了什么（如图片库勾选的素材）；离开页面时传 null
  setSelection: (s: Selection) => void
}

const Ctx = createContext<Assistant>(null as unknown as Assistant)
const KEY_OPEN = 'xs.assistant.open'
const KEY_MSGS = 'xs.assistant.msgs'

function read<T>(store: () => Storage, key: string, fallback: T): T {
  try { const v = store().getItem(key); return v ? JSON.parse(v) as T : fallback } catch { return fallback }
}
function write(store: () => Storage, key: string, v: unknown) {
  try { store().setItem(key, JSON.stringify(v)) } catch { /* 隐私模式等情况下存不了，忽略 */ }
}

// 地址 → 页面名，给助手当上下文
const PAGES: [RegExp, string][] = [
  [/^\/$/, '今天（首页）'], [/^\/products\/\d+/, '商品详情'], [/^\/products/, '商品库'], [/^\/assets/, '素材库'],
  [/^\/process/, '图片处理'], [/^\/copy/, '文案中心'], [/^\/templates/, '模板中心'], [/^\/review/, '出图审核'],
  [/^\/tasks/, '任务'], [/^\/kb/, '知识库'], [/^\/inventory/, '库存'], [/^\/purchase-orders/, '采购单'],
  [/^\/orders/, '订单'], [/^\/suppliers/, '供应商'], [/^\/assembly/, '组装加工'], [/^\/finance/, '财务'],
  [/^\/replenishment/, '补货建议'], [/^\/purchase-recognize/, '单据识别'], [/^\/models/, '模特库'], [/^\/tryon/, '试穿试戴'],
  [/^\/admin/, '账号与权限'], [/^\/settings/, '设置']
]

export function AssistantProvider({ children }: { children: ReactNode }) {
  const loc = useLocation()
  const [open, setOpenState] = useState<boolean>(() => read(() => localStorage, KEY_OPEN, false))
  const [messages, setMessages] = useState<ChatMsg[]>(() => read(() => sessionStorage, KEY_MSGS, []))
  const [busy, setBusy] = useState(false)
  const [picked, setSelection] = useState<Selection>(null)
  const live = useRef(messages)
  live.current = messages

  useEffect(() => { write(() => sessionStorage, KEY_MSGS, messages.slice(-40)) }, [messages])
  const setOpen = useCallback((v: boolean) => { setOpenState(v); write(() => localStorage, KEY_OPEN, v) }, [])

  // 详情页自动把当前对象交给助手，真实名称和权限由后端核对。
  const selection = useMemo<Selection>(() => {
    if (picked && picked.ids.length) return picked
    const routes = [
      [/^\/products\/(\d+)(?:\/|$)/, 'product'],
      [/^\/kb\/(\d+)$/, 'kb_article'],
      [/^\/inventory\/items\/(\d+)$/, 'inv_item'],
      [/^\/purchase-orders\/(\d+)$/, 'purchase_order']
    ] as const
    for (const [re, type] of routes) {
      const m = re.exec(loc.pathname)
      if (m) return { type, ids: [Number(m[1])] }
    }
    return null
  }, [picked, loc.pathname])

  const send = useCallback((text: string) => {
    const t = text.trim()
    if (!t || busy) return
    const history = [...live.current.filter(m => !m.error), { role: 'user' as const, content: t }]
    setMessages(m => [...m, { role: 'user', content: t }])
    setOpen(true)
    setBusy(true)
    const title = PAGES.find(([re]) => re.test(loc.pathname))?.[1] || loc.pathname
    api<AssistReply>('/api/assistant/chat', {
      body: {
        messages: history.slice(-20).map(m => ({ role: m.role, content: m.content.slice(0, 2000) })),
        context: { path: loc.pathname + loc.search, title, ...(selection ? { selection: { type: selection.type, ids: selection.ids.slice(0, 50) } } : {}) }
      }
    })
      .then(r => setMessages(m => [...m, { role: 'assistant', content: r.reply, actions: r.actions || [], refs: r.refs || [] }]))
      .catch(e => setMessages(m => [...m, { role: 'assistant', content: e instanceof Error ? e.message : String(e), error: true }]))
      .finally(() => setBusy(false))
  }, [busy, loc.pathname, loc.search, selection, setOpen])

  const clear = useCallback(() => setMessages([]), [])
  return <Ctx.Provider value={{ open, setOpen, messages, busy, send, clear, setSelection }}>{children}</Ctx.Provider>
}

export const useAssistant = () => useContext(Ctx)

// 执行一张动作卡：用当前登录用户调现有接口（权限、额度、日志都走原接口）。
// 返回后台任务（有的话）或执行后可以打开的页面（如新建的任务）。
export type ActionResult = { job?: Job; path?: string }
export async function runAction(a: AssistAction): Promise<ActionResult> {
  const p = a.params as Record<string, unknown>
  const id = Number(p.listing_id)
  const job = (r: unknown): ActionResult => (r && typeof r === 'object' && 'job' in r ? { job: (r as { job: Job }).job } : {})
  switch (a.type) {
    case 'run_listing': return job(await api(`/api/listings/${id}/run`, { method: 'POST' }))
    case 'render_listing': return job(await api(`/api/listings/${id}/render/run`, { method: 'POST' }))
    case 'render_batch': await api('/api/render/batch', { body: { listing_ids: p.listing_ids } }); return {}
    case 'process_assets': return job(await api('/api/assets/process', { body: { asset_ids: p.asset_ids, ops: p.ops, preset_id: p.preset_id ?? null } }))
    case 'generate_copy': return job(await api(`/api/listings/${id}/copies/generate`, { body: { instructions: p.instructions || '' } }))
    case 'review': await api(`/api/listings/${id}/review`, { body: { status: p.status, note: p.note || '' } }); return {}
    case 'create_task': {
      // 后端已校验并把 assignee_id 补全，这里只挑任务接口认识的字段
      const keys = ['title', 'body', 'assignee_id', 'priority', 'due_at', 'target_type', 'target_id', 'checklist', 'watcher_ids']
      const body = Object.fromEntries(keys.filter(k => p[k] !== undefined).map(k => [k, p[k]]))
      const r = await api<{ task: Task }>('/api/tasks', { body })
      window.dispatchEvent(new Event('xs:tasks-changed'))
      return { path: `/tasks/${r.task.id}` }
    }
    default: throw new Error('不认识的操作：' + a.type)
  }
}
