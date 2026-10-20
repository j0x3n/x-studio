import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { ExternalLink } from 'lucide-react'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { Topbar, Pill, Loading, Empty } from '../components/ui'
import { CompareView, ReviewDetail, TextRows, JudgeBar } from '../components/CreativeShared'
import { SLOT, creativeApi, minScore, slotName, type CreativeItem } from '../lib/creative'
import '../styles/creative.css'

// 复核队列：被转到人工的图按风险排序，左边列表，中间原图和生成图对比，右边复核结果和判断（键盘 1 通过、2 重画、3 弃用）
export function CreativeReview() {
  const { me, can } = useAuth()
  const toast = useToast()
  const shops = me?.shops || []
  const [shopId, setShopId] = useState(0)
  const [slot, setSlot] = useState('')
  const [items, setItems] = useState<CreativeItem[] | null>(null)
  const [total, setTotal] = useState(0)
  const [sel, setSel] = useState(0)
  const load = useCallback(() => creativeApi.review({ shop_id: shopId || undefined, slot: slot || undefined }).then(r => {
    setItems(r.items || []); setTotal(r.total || 0)
    setSel(id => (r.items || []).some(i => i.id === id) ? id : r.items?.[0]?.id || 0)
  }).catch(e => { toast(errText(e), 'error'); setItems([]) }), [shopId, slot, toast])
  useEffect(() => { load() }, [load])
  const it = items?.find(i => i.id === sel)
  // 判断完跳到下一张
  const next = () => {
    const idx = items?.findIndex(i => i.id === sel) ?? -1
    const rest = (items || []).filter(i => i.id !== sel)
    setItems(rest); setTotal(t => Math.max(0, t - 1))
    setSel(rest[Math.min(idx, rest.length - 1)]?.id || 0)
  }
  return (
    <>
      <Topbar title="复核队列" sub={items ? `${total} 张待人工 · 按风险排序` : undefined}>
        {shops.length > 1 && <select className="input" style={{ width: 140 }} value={shopId} onChange={e => setShopId(Number(e.target.value))}><option value={0}>全部店铺</option>{shops.map(s => <option key={s.id} value={s.id}>{s.name}</option>)}</select>}
        <select className="input" style={{ width: 120 }} value={slot} onChange={e => setSlot(e.target.value)}><option value="">全部位置</option>{Object.entries(SLOT).map(([k, v]) => <option key={k} value={k}>{v}</option>)}</select>
      </Topbar>
      {!items ? <Loading /> : !items.length ? <div className="scroll"><main className="content"><Empty title="没有要人工看的图" desc="AI 复核通过的图会自动放行；首图、多尺寸对比、校准期模特图和抽检会转到这里。" /></main></div> : (
        <div className="lib">
          <aside className="cr-queue">
            {items.map(i => {
              const s = minScore(i)
              return (
                <button key={i.id} type="button" className={'cr-q' + (i.id === sel ? ' active' : '')} onClick={() => setSel(i.id)}>
                  {i.output_url ? <img src={i.output_url} alt="" /> : <span className="cr-q-ph" />}
                  <span className="stack" style={{ gap: 2, minWidth: 0 }}>
                    <span className="cell-title ellipsis">{i.product_name || `上架版本 #${i.listing_id}`}</span>
                    <span className="fs-12 t3">{slotName(i.slot)}{i.variant ? ` ${i.variant}` : ''}{i.seq > 1 ? ` · ${i.seq}` : ''}{s !== null ? ` · ${s} 分` : ''}</span>
                    <span className="inline">{i.route_reasons.slice(0, 2).map(r => <span key={r} className="tag tag-gray">{r}</span>)}</span>
                  </span>
                </button>
              )
            })}
          </aside>
          <div className="lib-main"><div className="scroll"><div className="cr-stage">{it && <CompareView key={it.id} it={it} />}</div></div></div>
          <aside className="inspector lib-inspector cr-inspector">
            {it && <>
              <div className="inspector-body">
                <div className="inspector-section">
                  <div className="head"><span className="section-title">{slotName(it.slot)}{it.variant ? ` ${it.variant} 版` : ''}</span><Pill kind="pill-info">风险 {it.risk}</Pill></div>
                  {it.product_id && <Link className="link fs-12 inline" to={`/products/${it.product_id}`} title="在商品详情的 AI 出图里看整套"><ExternalLink size={12} />{it.product_name}</Link>}
                </div>
                <div className="inspector-section"><ReviewDetail it={it} /></div>
                <div className="inspector-section"><TextRows it={it} canEdit={can('creative.run')} onDone={load} /></div>
              </div>
              <div className="inspector-foot"><JudgeBar key={it.id} it={it} onDone={next} hotkeys /></div>
            </>}
          </aside>
        </div>
      )}
    </>
  )
}
