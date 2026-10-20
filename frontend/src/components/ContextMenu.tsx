import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { ChevronRight } from 'lucide-react'

// 右键菜单：在鼠标位置弹出，贴边时自动翻到另一侧；点外面、滚动、Esc 都会关掉。
export type MenuEntry =
  | { label: ReactNode; icon?: ReactNode; onClick?: () => void; danger?: boolean; disabled?: boolean; hint?: ReactNode; children?: MenuEntry[] }
  | 'sep'
  | { heading: ReactNode }

export function ContextMenu({ x, y, items, onClose }: { x: number; y: number; items: MenuEntry[]; onClose: () => void }) {
  useEffect(() => {
    const close = () => onClose()
    const key = (e: KeyboardEvent) => { if (e.key === 'Escape') { e.stopPropagation(); onClose() } }
    addEventListener('mousedown', close)
    addEventListener('blur', close)
    addEventListener('resize', close)
    addEventListener('wheel', close, { passive: true })
    addEventListener('keydown', key, true)
    return () => {
      removeEventListener('mousedown', close)
      removeEventListener('blur', close)
      removeEventListener('resize', close)
      removeEventListener('wheel', close)
      removeEventListener('keydown', key, true)
    }
  }, [onClose])
  return <MenuList x={x} y={y} items={items} onClose={onClose} />
}

function MenuList({ x, y, items, onClose, sub }: { x: number; y: number; items: MenuEntry[]; onClose: () => void; sub?: boolean }) {
  const ref = useRef<HTMLDivElement>(null)
  const [pos, setPos] = useState({ left: x, top: y, ready: false })
  const [open, setOpen] = useState<{ i: number; x: number; y: number } | null>(null)
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const { width, height } = el.getBoundingClientRect()
    let left = x, top = y
    if (left + width > innerWidth - 8) left = sub ? x - width - el.parentElement!.getBoundingClientRect().width + 8 : innerWidth - width - 8
    if (top + height > innerHeight - 8) top = Math.max(8, innerHeight - height - 8)
    setPos({ left: Math.max(8, left), top, ready: true })
  }, [x, y, sub])
  return (
    <div ref={ref} className="menu ctx-menu" role="menu" style={{ left: pos.left, top: pos.top, visibility: pos.ready ? 'visible' : 'hidden' }}
      onMouseDown={e => e.stopPropagation()} onContextMenu={e => e.preventDefault()}>
      {items.map((it, i) => {
        if (it === 'sep') return <div key={i} className="menu-sep" />
        if ('heading' in it) return <div key={i} className="menu-label">{it.heading}</div>
        const hasSub = !!it.children?.length
        return (
          <div key={i} role="menuitem" aria-disabled={it.disabled} className={'menu-item' + (it.danger ? ' danger' : '') + (it.disabled ? ' disabled' : '') + (open?.i === i ? ' open' : '')}
            onMouseEnter={e => {
              if (!hasSub || it.disabled) { setOpen(null); return }
              const r = e.currentTarget.getBoundingClientRect()
              setOpen({ i, x: r.right - 4, y: r.top - 4 })
            }}
            onClick={() => { if (it.disabled || hasSub) return; onClose(); it.onClick?.() }}>
            {it.icon}<span className="grow ellipsis">{it.label}</span>
            {it.hint && <span className="end">{it.hint}</span>}
            {hasSub && <ChevronRight className="end" />}
            {hasSub && open?.i === i && <MenuList x={open.x} y={open.y} items={it.children!} onClose={onClose} sub />}
          </div>
        )
      })}
    </div>
  )
}

// 右键菜单状态：const menu = useContextMenu(); onContextMenu={e => menu.open(e, items)}; {menu.node}
export function useContextMenu() {
  const [m, setM] = useState<{ x: number; y: number; items: MenuEntry[] } | null>(null)
  return {
    open: (e: { clientX: number; clientY: number; preventDefault: () => void; stopPropagation: () => void }, items: MenuEntry[]) => {
      e.preventDefault(); e.stopPropagation()
      setM({ x: e.clientX, y: e.clientY, items })
    },
    close: () => setM(null),
    node: m && <ContextMenu x={m.x} y={m.y} items={m.items} onClose={() => setM(null)} />
  }
}
