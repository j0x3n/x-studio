import { createContext, useCallback, useContext, useState, type ReactNode } from 'react'

type Kind = 'info' | 'ok' | 'error'
type Toast = { id: number; text: string; kind: Kind }
const Ctx = createContext<(text: string, kind?: Kind) => void>(() => {})
let seq = 0

export function ToastProvider({ children }: { children: ReactNode }) {
  const [list, setList] = useState<Toast[]>([])
  const push = useCallback((text: string, kind: Kind = 'info') => {
    const id = ++seq
    setList(l => [...l, { id, text, kind }])
    setTimeout(() => setList(l => l.filter(t => t.id !== id)), kind === 'error' ? 6000 : 3000)
  }, [])
  return (
    <Ctx.Provider value={push}>
      {children}
      <div className="toasts">{list.map(t => <div key={t.id} className={'toast ' + t.kind}>{t.text}</div>)}</div>
    </Ctx.Provider>
  )
}

export const useToast = () => useContext(Ctx)
// 把异常转成提示文字：toast(errText(e), 'error')
export const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))
