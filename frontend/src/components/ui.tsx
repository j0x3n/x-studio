import { useEffect, type ReactNode } from 'react'
import { X, Inbox, Sparkles } from 'lucide-react'
import { useAssistant } from '../lib/assistant'

// 顶栏 = 页头：上级面包屑 + 页面标题 + 一行摘要 + 本页操作 + 助手开关。
// 只传 crumbs 时最后一项当标题；传了 title 时 crumbs 全部当上级。
export function Topbar({ crumbs = [], title, sub, children }: { crumbs?: ReactNode[]; title?: ReactNode; sub?: ReactNode; children?: ReactNode }) {
  const assist = useAssistant()
  const parents = title === undefined ? crumbs.slice(0, -1) : crumbs
  const head = title === undefined ? crumbs[crumbs.length - 1] : title
  return (
    <header className="topbar">
      <div className="tb-head">
        {parents.length > 0 && (
          <span className="crumbs">
            {parents.map((c, i) => <span key={i} className="inline">{i > 0 && <span className="sep" />}{c}</span>)}
            <span className="sep" />
          </span>
        )}
        <h1 className="tb-title">{head}</h1>
        {sub && <span className="tb-sub">{sub}</span>}
      </div>
      <span className="grow" />
      {children}
      {assist && !assist.open && (
        <button className="btn btn-secondary assist-toggle" onClick={() => assist.setOpen(true)} title="打开 AI 助手"><Sparkles />助手</button>
      )}
    </header>
  )
}

export function Pill({ kind, children, dot = true }: { kind: string; children: ReactNode; dot?: boolean }) {
  return <span className={`pill ${dot ? 'dot ' : ''}${kind}`}>{children}</span>
}

export function Empty({ title, desc, children }: { title: string; desc?: string; children?: ReactNode }) {
  return (
    <div className="empty">
      <Inbox className="empty-icon" />
      <div className="empty-title">{title}</div>
      {desc && <div className="empty-desc">{desc}</div>}
      {children}
    </div>
  )
}

// 右侧抽屉：Esc 关闭
export function Drawer({ title, width = 420, onClose, children, foot }: { title: ReactNode; width?: number; onClose: () => void; children: ReactNode; foot?: ReactNode }) {
  useEffect(() => {
    const f = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    addEventListener('keydown', f)
    return () => removeEventListener('keydown', f)
  }, [onClose])
  return (
    <div className="mask" onMouseDown={e => e.target === e.currentTarget && onClose()}>
      <aside className="drawer" style={{ width }}>
        <div className="drawer-head">{title}<button className="close-btn" onClick={onClose} aria-label="关闭"><X /></button></div>
        <div className="drawer-body">{children}</div>
        {foot && <div className="drawer-foot">{foot}</div>}
      </aside>
    </div>
  )
}

export function Field({ label, req, hint, children }: { label: string; req?: boolean; hint?: string; children: ReactNode }) {
  return (
    <label className="form-field">
      <span className={'label' + (req ? ' req' : '')}>{label}</span>
      {children}
      {hint && <span className="help">{hint}</span>}
    </label>
  )
}

export function Loading() {
  return <div className="loading">加载中…</div>
}

export function Tabs<T extends string>({ value, onChange, items }: { value: T; onChange: (v: T) => void; items: { key: T; label: string; count?: number }[] }) {
  return (
    <div className="tabs">
      {items.map(t => (
        <button key={t.key} className={'tab' + (t.key === value ? ' active' : '')} onClick={() => onChange(t.key)}>
          {t.label}{t.count !== undefined && <span className="count">{t.count}</span>}
        </button>
      ))}
    </div>
  )
}
