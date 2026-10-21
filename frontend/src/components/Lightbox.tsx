import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'
import { X, ChevronLeft, ChevronRight, Download, ZoomIn, ZoomOut } from 'lucide-react'

// 站内看大图：所有「点开看原图」都走这里，不再新开浏览器窗口。
// ← → 切换，Esc 关闭，点图片在「适应窗口 / 原始大小」之间切换。
export type LightboxItem = { src: string; title?: ReactNode; meta?: ReactNode; download?: string; video?: boolean; poster?: string }
type Open = (items: LightboxItem[] | LightboxItem, index?: number) => void

const Ctx = createContext<Open>(() => {})
export const useLightbox = () => useContext(Ctx)

export function LightboxProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<{ items: LightboxItem[]; index: number } | null>(null)
  const open = useCallback<Open>((items, index = 0) => {
    const list = Array.isArray(items) ? items : [items]
    if (list.length) setState({ items: list, index: Math.min(Math.max(index, 0), list.length - 1) })
  }, [])
  return <Ctx.Provider value={open}>{children}{state && <Lightbox {...state} onIndex={i => setState(s => s && { ...s, index: i })} onClose={() => setState(null)} />}</Ctx.Provider>
}

function Lightbox({ items, index, onIndex, onClose }: { items: LightboxItem[]; index: number; onIndex: (i: number) => void; onClose: () => void }) {
  const [actual, setActual] = useState(false)
  const [loaded, setLoaded] = useState(false)
  const cur = items[index]
  const many = items.length > 1
  const go = useCallback((d: number) => { setActual(false); onIndex((index + d + items.length) % items.length) }, [index, items.length, onIndex])
  useEffect(() => setLoaded(false), [cur.src])
  useEffect(() => {
    const f = (e: KeyboardEvent) => {
      if (e.key === 'Escape') { e.stopPropagation(); onClose() }
      else if (e.key === 'ArrowLeft' && many) go(-1)
      else if (e.key === 'ArrowRight' && many) go(1)
    }
    // 捕获阶段处理，避免 Esc 同时关掉下面的抽屉
    addEventListener('keydown', f, true)
    return () => removeEventListener('keydown', f, true)
  }, [go, many, onClose])
  return (
    <div className="lightbox" role="dialog" aria-modal="true" aria-label="查看大图" onMouseDown={e => e.target === e.currentTarget && onClose()}>
      <div className="lightbox-bar">
        <div className="lightbox-title ellipsis">{cur.title}</div>
        {cur.meta && <span className="lightbox-meta">{cur.meta}</span>}
        {many && <span className="lightbox-meta mono">{index + 1} / {items.length}</span>}
        <span className="grow" />
        <button className="lightbox-btn" onClick={() => setActual(v => !v)} title={actual ? '适应窗口' : '原始大小'}>{actual ? <ZoomOut /> : <ZoomIn />}</button>
        {cur.download !== undefined && <a className="lightbox-btn" href={cur.download || cur.src} download title="下载"><Download /></a>}
        <button className="lightbox-btn" onClick={onClose} title="关闭（Esc）" aria-label="关闭"><X /></button>
      </div>
      <div className={'lightbox-stage' + (actual ? ' actual' : '')} onMouseDown={e => e.target === e.currentTarget && onClose()}>
        {!loaded && <span className="lightbox-loading">加载中…</span>}
        {cur.video
          ? <video key={cur.src} src={cur.src} poster={cur.poster || undefined} controls autoPlay playsInline onLoadedData={() => setLoaded(true)} onError={() => setLoaded(true)} style={loaded ? undefined : { opacity: 0 }} />
          : <img key={cur.src} src={cur.src} alt="" onLoad={() => setLoaded(true)} onClick={() => setActual(v => !v)} style={loaded ? undefined : { opacity: 0 }} />}
      </div>
      {many && <>
        <button className="lightbox-nav prev" onClick={() => go(-1)} aria-label="上一张"><ChevronLeft /></button>
        <button className="lightbox-nav next" onClick={() => go(1)} aria-label="下一张"><ChevronRight /></button>
      </>}
    </div>
  )
}
