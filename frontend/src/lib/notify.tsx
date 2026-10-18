import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'
import { api } from './api'
import type { Notice } from './types'

type Ctx = {
  unread: number
  latest: Notice | null // 最近推来的一条，铃铛用来刷新列表
  desktop: NotificationPermission | 'unsupported'
  enableDesktop: () => Promise<void>
  markRead: (ids: number[] | 'all') => Promise<void>
}
const NotifyCtx = createContext<Ctx>({ unread: 0, latest: null, desktop: 'unsupported', enableDesktop: async () => {}, markRead: async () => {} })

// 一个页面一条 SSE 连接：收未读数和新通知，按需弹系统通知。断了浏览器会自动重连。
export function NotifyProvider({ children }: { children: ReactNode }) {
  const nav = useNavigate()
  const [unread, setUnread] = useState(0)
  const [latest, setLatest] = useState<Notice | null>(null)
  const [desktop, setDesktop] = useState<Ctx['desktop']>(() => (typeof Notification === 'undefined' ? 'unsupported' : Notification.permission))
  const navRef = useRef(nav)
  navRef.current = nav

  useEffect(() => {
    if (typeof EventSource === 'undefined') return
    const es = new EventSource('/api/notifications/stream', { withCredentials: true })
    es.addEventListener('unread', e => { try { setUnread(JSON.parse((e as MessageEvent).data).count || 0) } catch { /* 忽略坏数据 */ } })
    es.addEventListener('notification', e => {
      let n: Notice
      try { n = JSON.parse((e as MessageEvent).data) } catch { return }
      setLatest(n)
      setUnread(c => c + 1)
      // 多个标签页都会收到，用同一个 tag 让系统只显示一条
      if (n.desktop !== false && typeof Notification !== 'undefined' && Notification.permission === 'granted') {
        const sys = new Notification(n.title, { body: n.body, tag: `xs-${n.id}`, icon: '/favicon.svg' })
        sys.onclick = () => { window.focus(); if (n.link) navRef.current(n.link); sys.close() }
      }
    })
    return () => es.close()
  }, [])

  const enableDesktop = useCallback(async () => {
    if (typeof Notification === 'undefined') return
    setDesktop(await Notification.requestPermission())
  }, [])
  const markRead = useCallback(async (ids: number[] | 'all') => {
    await api('/api/notifications/read', { body: ids === 'all' ? { all: true } : { ids } })
    const r = await api<{ count: number }>('/api/notifications/unread-count')
    setUnread(r.count)
  }, [])

  return <NotifyCtx.Provider value={{ unread, latest, desktop, enableDesktop, markRead }}>{children}</NotifyCtx.Provider>
}
export const useNotify = () => useContext(NotifyCtx)
