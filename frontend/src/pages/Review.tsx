import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Check, X, Package, PenLine, ExternalLink, Images, FileImage, ClipboardCheck } from 'lucide-react'
import { api, type Page } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import type { DetailTemplate, QueueItem } from '../lib/types'
import { Topbar, Pill, Loading, Empty } from '../components/ui'
import { fmtAgo, fmtDate } from '../lib/format'
import { useLightbox } from '../components/Lightbox'

const REJECT_REASONS = ['文案有错', '图片不清晰', '颜色和实物不符', 'AI 痕迹明显', '模板不合适', '照片顺序不对']

export function Review() {
  const { me, can } = useAuth()
  const toast = useToast()
  const lightbox = useLightbox()
  const nav = useNavigate()
  const shops = me?.shops || []
  const [shop, setShop] = useState('')
  const [items, setItems] = useState<QueueItem[] | null>(null)
  const [sel, setSel] = useState(0)
  const [view, setView] = useState<'images' | 'page'>('images')
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)
  const [tplNames, setTplNames] = useState<Record<string, string>>({})
  const [requireReview, setRequireReview] = useState<boolean | null>(null)

  const load = useCallback(() => {
    api<Page<QueueItem>>('/api/review/queue' + (shop ? `?shop_id=${shop}` : '') + `${shop ? '&' : '?'}page_size=200`).then(r => {
      setItems(r.items)
      setSel(s => (r.items.some(i => i.listing_id === s) ? s : r.items[0]?.listing_id || 0))
    }).catch(e => { toast(errText(e), 'error'); setItems([]) })
  }, [shop, toast])
  useEffect(() => { load() }, [load])
  useEffect(() => {
    api<{ detail: DetailTemplate[] }>('/api/templates').then(d => setTplNames(Object.fromEntries(d.detail.map(t => [t.key, t.name])))).catch(() => {})
    api<{ require_review: boolean }>('/api/studio/settings').then(r => setRequireReview(r.require_review)).catch(() => {})
  }, [])
  useEffect(() => { setNote('') }, [sel])

  const cur = items?.find(i => i.listing_id === sel)
  const decide = async (status: 'approved' | 'rejected') => {
    if (!cur) return
    if (status === 'rejected' && !note.trim()) return toast('驳回要写一下原因，方便同事改', 'error')
    setBusy(true)
    try {
      await api(`/api/listings/${cur.listing_id}/review`, { body: { status, note: note.trim() } })
      toast(status === 'approved' ? `「${cur.product_name}」已通过，可以下载了` : `已驳回「${cur.product_name}」`, 'ok')
      // 审完自动跳到下一款
      const idx = items!.findIndex(i => i.listing_id === cur.listing_id)
      const rest = items!.filter(i => i.listing_id !== cur.listing_id)
      setItems(rest)
      setSel(rest[Math.min(idx, rest.length - 1)]?.listing_id || 0)
    } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const toggleRequire = async (v: boolean) => {
    try { const r = await api<{ require_review: boolean }>('/api/studio/settings', { method: 'PATCH', body: { require_review: v } }); setRequireReview(r.require_review); toast(v ? '出图后要审核通过才能下载' : '出图后直接可以下载', 'ok') } catch (e) { toast(errText(e), 'error') }
  }
  const tplName = (k: string) => tplNames[k] || k || '默认'
  const addReason = (r: string) => setNote(v => (v.includes(r) ? v : v ? `${v}；${r}` : r))

  return (
    <>
      <Topbar crumbs={['出图审核', ...(cur ? [`${cur.product_name} · ${cur.shop_name}`] : [])]} />
      <div className="lib">
        <aside className="sidebar lib-filters">
          {shops.length > 1 && (
            <div className="nav-group">
              <div className="nav-label">店铺</div>
              <select className="input" value={shop} onChange={e => setShop(e.target.value)}>
                <option value="">全部店铺</option>
                {shops.map(s => <option key={s.id} value={s.id}>{s.name}</option>)}
              </select>
            </div>
          )}
          <div className="nav-group" style={{ minHeight: 0, flex: 1 }}>
            <div className="nav-label">待审核<span className="end">{items?.length ?? ''}</span></div>
            <div className="proc-list stack pick-list">
              {!items ? <div className="t3 fs-12" style={{ padding: 8 }}>加载中…</div> : items.length === 0 ? <div className="t3 fs-12" style={{ padding: 8 }}>都审完了</div> : items.map(i => (
                <button key={i.listing_id} className={'proc-item' + (i.listing_id === sel ? ' active' : '')} onClick={() => setSel(i.listing_id)}>
                  {i.outputs.main34[0] ? <img src={i.outputs.main34[0] + '?w=240'} alt="" /> : <span className="cover"><Package /></span>}
                  <span style={{ minWidth: 0, flex: 1 }}>
                    <span className="cell-title ellipsis" style={{ display: 'block' }}>{i.product_name}</span>
                    <span className="cell-sub ellipsis" style={{ display: 'block' }}>{i.shop_name} · {fmtAgo(i.updated_at)}</span>
                  </span>
                </button>
              ))}
            </div>
          </div>
          {can('admin.settings') && requireReview !== null && (
            <div className="sidebar-foot">
              <label className="setting fs-12"><span className="t2">出图后要审核才能下载</span><input type="checkbox" className="switch" checked={requireReview} onChange={e => toggleRequire(e.target.checked)} /></label>
            </div>
          )}
        </aside>

        {!items ? <div className="lib-main"><Loading /></div> : !cur ? (
          <div className="lib-main"><Empty title="没有待审核的成品" desc={requireReview === false ? '现在设置成出图后直接可以下载，不用审核。' : '有人出图后会出现在这里，审核通过才能下载成品。'} /></div>
        ) : <>
          <div className="lib-main">
            <div className="lib-toolbar">
              <div className="segmented">
                <button className={view === 'images' ? 'active' : ''} onClick={() => setView('images')}><Images />成品图</button>
                <button className={view === 'page' ? 'active' : ''} onClick={() => setView('page')}><FileImage />详情页长图</button>
              </div>
              <span className="t3 fs-12">主图 {cur.outputs.main34.length} 张 · 详情页 {cur.outputs.detail.length} 段{cur.outputs.white ? ' · 白底图 1 张' : ''}</span>
              <span className="spacer" />
              <button className="btn btn-ghost btn-sm" onClick={() => nav(`/products/${cur.product_id}`)}><ExternalLink />商品页</button>
            </div>
            <div className="scroll">
              {view === 'images' ? (
                <div className="copy-body">
                  <OutSection title="主图 3:4" urls={cur.outputs.main34} big />
                  <OutSection title="主图 1:1" urls={cur.outputs.main11} />
                  {cur.outputs.white && <OutSection title="白底图" urls={[cur.outputs.white]} />}
                  {cur.outputs.main34.length + cur.outputs.main11.length === 0 && !cur.outputs.white && cur.outputs.detail.length === 0 && <Empty title="成品文件不见了" desc="可能被清理过，回商品页重新出图。" />}
                </div>
              ) : (
                <div className="review-long">
                  {cur.outputs.detail.length === 0 ? <Empty title="没有详情页切片" /> : cur.outputs.detail.map((u, n, all) => <button key={u} type="button" className="zoomable" onClick={() => lightbox(all.map((x, k) => ({ src: x, title: `详情页 · 第 ${k + 1} 屏`, download: '' })), n)}><img src={u} alt="" loading="lazy" /></button>)}
                </div>
              )}
            </div>
          </div>
          <aside className="inspector lib-inspector">
            <div className="inspector-body">
              <div className="inspector-section">
                <div className="head"><span className="section-title ellipsis">{cur.product_name}</span><Pill kind="pill-warning">待审核</Pill></div>
                <dl className="kv narrow">
                  <dt>店铺</dt><dd>{cur.shop_name}</dd>
                  <dt>模板</dt><dd>{tplName(cur.rendered_template || cur.detail_template)}</dd>
                  <dt>出图</dt><dd>{fmtDate(cur.updated_at, true)}</dd>
                </dl>
                <a className="link fs-12 inline" onClick={() => nav(`/copy?listing=${cur.listing_id}`)}><PenLine size={13} />文案有问题？去文案中心改</a>
              </div>
              <div className="inspector-section">
                <div className="head"><span className="sub-title">审核意见</span><span className="t4 fs-12">驳回必填</span></div>
                <textarea className="textarea" rows={4} maxLength={1000} value={note} onChange={e => setNote(e.target.value)} placeholder="写给出图的同事看，如：第 3 张主图偏黄" />
                <div className="inline">{REJECT_REASONS.map(r => <button key={r} className="chip outline" onClick={() => addReason(r)}>{r}</button>)}</div>
              </div>
            </div>
            <div className="inspector-foot review-actions">
              <button className="btn btn-secondary" disabled={busy} onClick={() => decide('rejected')}><X />驳回</button>
              <button className="btn btn-primary" disabled={busy} onClick={() => decide('approved')}><Check />通过</button>
            </div>
          </aside>
        </>}
      </div>
    </>
  )
}

function OutSection({ title, urls, big }: { title: string; urls: string[]; big?: boolean }) {
  const lightbox = useLightbox()
  if (!urls.length) return null
  return (
    <section className="panel">
      <div className="panel-head"><ClipboardCheck className="t3" size={16} /><div className="panel-title">{title}</div><span className="t3 fs-12">{urls.length} 张</span></div>
      <div className="panel-pad">
        <div className={'out-grid' + (big ? ' big' : '')}>{urls.map((u, n) => (
          <button key={u} type="button" className="thumb" onClick={() => lightbox(urls.map((x, k) => ({ src: x, title: `${title} · 第 ${k + 1} 张`, download: '' })), n)}><img src={u + '?w=480'} alt="" loading="lazy" /><div className="meta"><span className="mono t3">{n + 1}</span></div></button>
        ))}</div>
      </div>
    </section>
  )
}
