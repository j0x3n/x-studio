import { useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { X, Smartphone, Monitor, ChevronLeft, ChevronRight, Store, MessageCircle, Star, Play } from 'lucide-react'
import type { Asset, Product, Shop } from '../lib/types'
import { itemOk, slotName, type CreativeItem, type CreativeSet } from '../lib/creative'
import { previewStyle, shopLabel } from '../lib/shops'

// 模拟商品页：按店铺平台排出手机商品页（主图轮播 → 价格标题 → 规格 → 详情长图），出完图导出前看整体效果。
// 只模仿版式，不放平台 logo；没出好的位置直接标出来。
type Slide = { src: string; label: string; warn?: string; video?: boolean; poster?: string }

const MAIN_ORDER = ['main1', 'main2', 'main3', 'main4', 'main5']
const DETAIL_ORDER = ['d1', 'd2', 'd3', 'd4', 'd5', 'd6', 'd7', 'd8', 'd9']

function warnOf(it: CreativeItem) {
  if (!it.output_url) return it.error ? '没出来' : '还在生成'
  if (it.human === 'discard') return '已弃用'
  if (itemOk(it)) return ''
  return it.human === 'redraw' ? '等重画' : '还没人看'
}

export function ListingPreview({ set, product, shop, videos, onClose }: { set: CreativeSet; product: Product; shop?: Shop; videos: Asset[]; onClose: () => void }) {
  const [device, setDevice] = useState<'phone' | 'desktop'>('phone')
  const [showWarn, setShowWarn] = useState(true)
  const style = previewStyle(shop)
  const listing = product.listings.find(l => l.id === set.listing_id)
  const meta = (listing?.meta || {}) as { title?: string }
  const content = (listing?.content || {}) as { title?: string; sku?: { name?: string }[] }
  const title = meta.title || content.title || product.name
  const price = (product.info as { price?: number } | undefined)?.price
  const cur = set.items.filter(i => !i.superseded && i.human !== 'discard')

  const { slides, missing, details, skus } = useMemo(() => {
    const missing: string[] = []
    const slides: Slide[] = videos.slice(0, 1).map(v => ({ src: v.url, label: '主图视频', video: true, poster: v.poster_url }))
    for (const slot of MAIN_ORDER) {
      const list = cur.filter(i => i.slot === slot)
      // 首图有 A/B 两版时只放 A 版（B 版设为 A 版后会换过来）
      const it = slot === 'main1' ? (list.find(i => i.variant === 'A' || !i.variant) || list[0]) : list.sort((a, b) => a.seq - b.seq)[0]
      if (!it) { missing.push(`主图 ${slotName(slot)}`); continue }
      if (it.output_url) slides.push({ src: it.output_url, label: slotName(slot), warn: warnOf(it) })
      else missing.push(`主图 ${slotName(slot)}（${warnOf(it)}）`)
    }
    const details: Slide[] = []
    for (const slot of DETAIL_ORDER) {
      for (const it of cur.filter(i => i.slot === slot).sort((a, b) => a.seq - b.seq)) {
        if (it.output_url) details.push({ src: it.output_url, label: `第 ${slot.slice(1)} 屏 ${slotName(slot)}`, warn: warnOf(it) })
        else missing.push(`详情 ${slotName(slot)}（${warnOf(it)}）`)
      }
    }
    if (set.parts?.includes('detail') && !details.length) missing.push('详情页一屏都没有')
    const skus = cur.filter(i => i.slot === 'sku' && i.output_url).sort((a, b) => a.seq - b.seq)
      .map((it, n) => ({ src: it.output_url!, label: content.sku?.[n]?.name || `规格 ${n + 1}`, warn: warnOf(it) }))
    return { slides, missing, details, skus }
  }, [cur, videos, set.parts, content.sku])

  const [i, setI] = useState(0)
  const track = useRef<HTMLDivElement>(null)
  const go = (n: number) => { const k = (n + slides.length) % Math.max(1, slides.length); setI(k); track.current?.scrollTo({ left: k * track.current.clientWidth, behavior: 'smooth' }) }
  useEffect(() => {
    const f = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    addEventListener('keydown', f); return () => removeEventListener('keydown', f)
  }, [onClose])
  const warnTag = (s: Slide) => showWarn && s.warn ? <span className="pv-warn">{s.warn}</span> : null
  const ratio = style === 'shopee' ? '1 / 1' : '3 / 4'

  return createPortal(
    <div className="pv-overlay" onMouseDown={e => e.target === e.currentTarget && onClose()}>
      <div className="pv-bar">
        <b>预览商品页</b>
        <span className="t3 fs-12">{shop ? `${shop.name} · ${shopLabel(shop)}版式` : '淘宝版式'} · 第 {set.version} 套</span>
        <span className="grow" />
        <label className="filter-opt fs-12"><input type="checkbox" className="check" checked={showWarn} onChange={e => setShowWarn(e.target.checked)} />标出没确认的图</label>
        <div className="segmented">
          <button className={device === 'phone' ? 'active' : ''} onClick={() => setDevice('phone')}><Smartphone />手机</button>
          <button className={device === 'desktop' ? 'active' : ''} onClick={() => setDevice('desktop')}><Monitor />电脑</button>
        </div>
        <button className="btn btn-ghost btn-icon btn-sm" onClick={onClose} aria-label="关闭"><X /></button>
      </div>
      <div className="pv-body">
        {missing.length > 0 && <aside className="pv-missing">
          <div className="pv-missing-title">这一套还缺</div>
          <ul>{missing.map(m => <li key={m}>{m}</li>)}</ul>
          <div className="t4 fs-12">缺的位置导出时会跳过；要补就回「AI 出图」重画或补拍照片。</div>
        </aside>}
        <div className={`pv-device ${device} pv-${style}`}>
          <div className="pv-scroll">
            <div className="pv-carousel" style={{ aspectRatio: ratio }}>
              <div className="pv-track" ref={track} onScroll={e => setI(Math.round(e.currentTarget.scrollLeft / Math.max(1, e.currentTarget.clientWidth)))}>
                {slides.map((s, n) => <div key={n} className="pv-slide">
                  {s.video ? <video src={s.src} poster={s.poster || undefined} controls playsInline /> : <img src={s.src} alt={s.label} />}
                  {warnTag(s)}
                </div>)}
                {!slides.length && <div className="pv-slide pv-empty">还没有主图</div>}
              </div>
              {slides.length > 1 && <>
                <button className="pv-nav prev" onClick={() => go(i - 1)} aria-label="上一张"><ChevronLeft /></button>
                <button className="pv-nav next" onClick={() => go(i + 1)} aria-label="下一张"><ChevronRight /></button>
                <span className="pv-count mono">{slides[i]?.video && <Play />}{i + 1}/{slides.length}</span>
              </>}
            </div>
            <div className="pv-thumbs">{slides.map((s, n) => <button key={n} className={n === i ? 'on' : ''} onClick={() => go(n)}><img src={s.video ? s.poster || '' : s.src} alt="" /></button>)}</div>

            <div className="pv-card">
              {style === 'shopee' ? <>
                <div className="pv-title">{title}</div>
                <div className="pv-price">{price ? `${shop?.currency || ''} ${price}` : '价格未填'}</div>
              </> : <>
                <div className="pv-price">{price ? <><small>¥</small>{price}</> : <span className="fs-13">价格未填</span>}</div>
                <div className="pv-title">{title}</div>
              </>}
              <div className="pv-tags"><span>包邮</span><span>7 天无理由</span><span>运费险</span></div>
            </div>

            {skus.length > 0 && <div className="pv-card">
              <div className="pv-row-title">{style === 'shopee' ? '规格' : '选择 颜色分类'}</div>
              <div className="pv-skus">{skus.map((s, n) => <div key={n} className="pv-sku"><img src={s.src} alt="" />{warnTag(s)}<span className="ellipsis">{s.label}</span></div>)}</div>
            </div>}

            <div className="pv-card pv-muted">
              <div className="pv-row-title inline"><Star size={14} />{style === 'shopee' ? '商品评价' : '宝贝评价'}<span className="t4">（预览不显示真实评价）</span></div>
            </div>

            <div className="pv-card pv-detail-head">{style === 'shopee' ? '商品详情' : '宝贝详情'}</div>
            <div className="pv-detail">
              {details.map((s, n) => <div key={n} className="pv-detail-img"><img src={s.src} alt={s.label} loading="lazy" />{warnTag(s)}</div>)}
              {!details.length && <div className="pv-empty">没有详情图</div>}
            </div>
            <div className="pv-end">— 到底了 —</div>
          </div>
          <div className="pv-buybar">
            <span className="pv-icon"><Store />店铺</span>
            <span className="pv-icon"><MessageCircle />客服</span>
            <span className="grow" />
            <span className="pv-btn cart">加入购物车</span>
            <span className="pv-btn buy">立即购买</span>
          </div>
        </div>
      </div>
    </div>, document.body)
}
