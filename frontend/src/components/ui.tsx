import { useEffect, type ReactNode } from 'react'
import { X, Inbox } from 'lucide-react'

// 顶栏：面包屑 + 右侧操作
export function Topbar({ crumbs, children }: { crumbs: ReactNode[]; children?: ReactNode }) {
  return (
    <header className="topbar">
      <div className="crumbs">
        {crumbs.map((c, i) => (
          <span key={i} className={i === crumbs.length - 1 ? 't1' : undefined}>
            {i > 0 && <span className="sep" />}{c}
          </span>
        ))}
      </div>
      <span className="grow" />
      {children}
    </header>
  )
}

// 页头：标题 + 摘要 + 右侧工具
export function PageHead({ title, sub, children }: { title: ReactNode; sub?: ReactNode; children?: ReactNode }) {
  return (
    <div className="page-head">
      <div>
        <h1 className="page-title">{title}</h1>
        {sub && <p className="page-sub">{sub}</p>}
      </div>
      {children && <div className="toolbar">{children}</div>}
    </div>
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
