import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'
import { api } from './api'
import type { Me } from './types'

type Auth = {
  me: Me | null
  loading: boolean
  can: (perm: string) => boolean
  isAdmin: boolean
  refresh: () => Promise<void>
  logout: () => Promise<void>
}

const Ctx = createContext<Auth>(null as unknown as Auth)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [me, setMe] = useState<Me | null>(null)
  const [loading, setLoading] = useState(true)
  const refresh = useCallback(async () => {
    try { setMe(await api<Me>('/api/me')) } catch { setMe(null) } finally { setLoading(false) }
  }, [])
  useEffect(() => { if (location.pathname !== '/login') refresh(); else setLoading(false) }, [refresh])
  const isAdmin = me?.role.key === 'admin'
  const can = useCallback((p: string) => !!me && (isAdmin || me.perms.includes(p)), [me, isAdmin])
  const logout = useCallback(async () => {
    try { await api('/api/logout', { method: 'POST' }) } finally { location.assign('/login') }
  }, [])
  return <Ctx.Provider value={{ me, loading, can, isAdmin, refresh, logout }}>{children}</Ctx.Provider>
}

export const useAuth = () => useContext(Ctx)
