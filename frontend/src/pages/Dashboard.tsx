import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Package, AlertTriangle, Sparkles, ClipboardCheck, Camera, PenLine, ChevronRight, ImageOff, type LucideIcon } from 'lucide-react'
import { api } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useAssistant, runAction } from '../lib/assistant'
import { useToast, errText } from '../lib/toast'
import type { Home, Todo } from '../lib/types'
import { Topbar, Loading, Empty } from '../components/ui'
import { fmtAgo, sourceName } from '../lib/format'

type Usage = { items: { kind: string; units: number; est_cost: number }[] }

const TODO_LOOK: Record<Todo['kind'], { icon: LucideIcon; tint: string }> = {
  render_failed: { icon: AlertTriangle, tint: 'tint-red' },
  review_pending: { icon: ClipboardCheck, tint: 'tint-purple' },
  copy_unapplied: { icon: PenLine, tint: 'tint-blue' },
  no_photo: { icon: Camera, tint: 'tint-orange' }
}
const WEEK = ['周日', '周一', '周二', '周三', '周四', '周五', '周六']
const greet = () => { const h = new Date().getHours(); return h < 6 ? '夜深了' : h < 11 ? '早上好' : h < 14 ? '中午好' : h < 18 ? '下午好' : '晚上好' }

// 首页「今天」：要处理的事按紧急程度排好，可以直接点按钮处理，也可以交给助手
export function Dashboard() {
  const { me } = useAuth()
  const nav = useNavigate()
  const toast = useToast()
  const assist = useAssistant()
  const [home, setHome] = useState<Home | null>(null)
  const [err, setErr] = useState('')
  const [usage, setUsage] = useState<Usage | null>(null)
  const [busy, setBusy] = useState<number | null>(null)

  const load = useCallback(() => {
    api<Home>('/api/home').then(h => { setHome(h); setErr('') }).catch(e => setErr(errText(e)))
    api<Usage>('/api/usage/me').then(setUsage).catch(() => setUsage(null))
  }, [])
  useEffect(() => { load() }, [load])

  const name = me?.user.display_name || me?.user.username
  const now = new Date()
  const date = `${now.getMonth() + 1} 月 ${now.getDate()} 日 · ${WEEK[now.getDay()]}`

  if (err) return <><Topbar title="今天" /><div className="scroll"><main className="content"><Empty title="首页数据加载失败" desc={err} /></main></div></>
  if (!home) return <><Topbar title="今天" /><Loading /></>
  const c = home.counts
  const units = (k: string) => (usage?.items || []).filter(i => i.kind === k).reduce((a, b) => a + (b.units || 0), 0)
  const cost = (usage?.items || []).reduce((a, b) => a + (b.est_cost || 0), 0)

  const doTodo = async (t: Todo, i: number) => {
    const a = t.action
    if (!a) return
    if (a.type === 'open') { nav(String(a.params.path || '/')); return }
    setBusy(i)
    try { await runAction(a); toast(`已开始：${a.label}`, 'ok'); load() } catch (e) { toast(errText(e), 'error') } finally { setBusy(null) }
  }

  return (
    <>
      <Topbar title={`${greet()}，${name}`} sub={<>{date}<span className="sep" />{home.todos.length ? `${home.todos.length} 件事要处理` : '暂时没有要处理的事'}</>}>
        <button className="btn btn-secondary" onClick={() => assist.send('今天先做什么？按紧急程度说，能直接处理的给我操作。')}><Sparkles />让助手排一下</button>
      </Topbar>
      <div className="scroll"><main className="content">
        <section className="kpi-grid">
          <div className="kpi">
            <div className="kpi-top"><span className="kpi-label">在途商品</span><span className="icon-tile sm tint-blue"><Package /></span></div>
            <div className="kpi-value mono">{c.in_progress}</div>
            <div className="kpi-foot">拍样中和待上架</div>
          </div>
          {c.review_pending !== null ? (
            <div className="kpi">
              <div className="kpi-top"><span className="kpi-label">待审核</span><span className="icon-tile sm tint-purple"><ClipboardCheck /></span></div>
              <div className="kpi-value mono">{c.review_pending}</div>
              <div className="kpi-foot"><a className="link" onClick={() => nav('/review')}>去审核</a></div>
            </div>
          ) : (
            <div className="kpi">
              <div className="kpi-top"><span className="kpi-label">还没传照片</span><span className="icon-tile sm tint-orange"><Camera /></span></div>
              <div className="kpi-value mono">{c.no_photo}</div>
              <div className="kpi-foot">未归档的商品</div>
            </div>
          )}
          <div className="kpi">
            <div className="kpi-top"><span className="kpi-label">出图失败</span><span className="icon-tile sm tint-red"><AlertTriangle /></span></div>
            <div className="kpi-value mono">{c.failed}</div>
            <div className="kpi-foot">{c.failed ? '看原因后重跑' : '没有失败的'}</div>
          </div>
          <div className="kpi">
            <div className="kpi-top"><span className="kpi-label">我本月的 AI 用量</span><span className="icon-tile sm tint-green"><Sparkles /></span></div>
            <div className="kpi-value mono">{units('image')}<span className="unit">张图</span></div>
            <div className="kpi-foot">文本 <span className="mono">{units('text')}</span> 次{cost > 0 && <> · 约 <span className="mono">¥{cost.toFixed(2)}</span></>}</div>
          </div>
        </section>

        <div className="home-grid">
          <section className="panel">
            <div className="panel-head">
              <div className="titles"><div className="panel-title">下一步</div><div className="panel-sub">按紧急程度排好了</div></div>
              {home.todos.length > 0 && <span className="chip">{home.todos.length} 件</span>}
            </div>
            {home.todos.length === 0 ? <Empty title="暂时没有要处理的" desc="出图失败、等审核、AI 写好的文案、缺照片的商品都会出现在这里。" /> : (
              <div className="next-list">
                {home.todos.map((t, i) => {
                  const look = TODO_LOOK[t.kind] || TODO_LOOK.no_photo
                  return (
                    <div key={i} className={'next-item' + (t.level === 'danger' ? ' danger' : '')}>
                      <span className={'icon-tile sm ' + look.tint}><look.icon /></span>
                      <div className="grow" style={{ minWidth: 0 }}>
                        <div className="title ellipsis">{t.title}</div>
                        <div className="meta ellipsis" title={t.detail}>{t.detail}</div>
                      </div>
                      {t.product_id && !t.action && <button className="btn btn-ghost btn-sm" onClick={() => nav(`/products/${t.product_id}`)}>查看<ChevronRight /></button>}
                      {t.action && (t.action.type === 'open'
                        ? <button className="btn btn-ghost btn-sm" onClick={() => doTodo(t, i)}>{t.action.label || '去处理'}<ChevronRight /></button>
                        : <button className="btn btn-secondary btn-sm" disabled={busy === i} onClick={() => doTodo(t, i)}>{t.action.label}</button>)}
                    </div>
                  )
                })}
              </div>
            )}
          </section>

          <section className="panel">
            <div className="panel-head">
              <div className="titles"><div className="panel-title">最近产出</div><div className="panel-sub">新上传和处理出来的素材</div></div>
              <button className="btn btn-ghost btn-sm" onClick={() => nav('/assets')}>素材库<ChevronRight /></button>
            </div>
            {home.recent_assets.length === 0 ? <Empty title="还没有素材" /> : (
              <div className="recent-grid">
                {home.recent_assets.map(a => {
                  const s = sourceName[a.source]
                  return (
                    <button key={a.id} onClick={() => nav(`/assets?product_id=${a.product_id}`)} title={`${a.product_name} · ${fmtAgo(a.created_at)}`}>
                      <span className="img">
                        {a.thumb_url ? <img src={a.thumb_url} alt="" loading="lazy" /> : <span className="cover"><ImageOff /></span>}
                        {s && a.source !== 'original' && <span className={'pill ' + s.pill}>{s.label}</span>}
                      </span>
                      <span className="cap ellipsis">{a.product_name}</span>
                    </button>
                  )
                })}
              </div>
            )}
          </section>
        </div>
      </main></div>
    </>
  )
}
