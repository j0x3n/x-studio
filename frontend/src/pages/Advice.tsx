import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { RefreshCw, MessageSquareText, ListPlus, Check, X, ExternalLink } from 'lucide-react'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { fmtAgo } from '../lib/format'
import { Topbar, Pill, Loading, Empty, Tabs } from '../components/ui'
import { NewTaskDrawer } from '../components/TaskViews'
import { LEVEL, RULE, marketApi, type Advice as Row } from '../lib/market'
import '../styles/market.css'

type Status = 'open' | 'done' | 'ignored' | 'expired'

// 运营建议：程序按规则算出来的诊断，每条分「事实 / 推测 / 缺什么数据」三栏；AI 只负责解释，广告操作由人在万相台做
export function Advice() {
  const { me, can } = useAuth()
  const toast = useToast()
  const shops = me?.shops || []
  const [shopId, setShopId] = useState(shops[0]?.id || 0)
  const [status, setStatus] = useState<Status>('open')
  const [rows, setRows] = useState<Row[] | null>(null)
  const [busy, setBusy] = useState(false)
  const [task, setTask] = useState<Row | null>(null)
  const load = useCallback(() => marketApi.advice({ shop_id: shopId || undefined, status }).then(r => setRows(r.items || [])).catch(e => { toast(errText(e), 'error'); setRows([]) }), [shopId, status, toast])
  useEffect(() => { load() }, [load])
  const recompute = async () => {
    setBusy(true)
    try { await marketApi.recompute(shopId); toast('已排队重算，几秒后刷新', 'ok'); setTimeout(load, 3000) } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const patch = async (r: Row, body: { status?: 'done' | 'ignored'; task_id?: number }) => {
    try { await marketApi.patchAdvice(r.id, body); load() } catch (e) { toast(errText(e), 'error') }
  }
  // 按款分组，同一款的建议放一起
  const groups = new Map<string, Row[]>()
  for (const r of rows || []) {
    const k = r.listing_id ? `${r.listing_id}` : 'shop'
    groups.set(k, [...(groups.get(k) || []), r])
  }
  return (
    <>
      <Topbar title="运营建议" sub="数字由程序按规则算，缺数据的不估算；广告怎么调由你在万相台操作">
        {shops.length > 1 && <select className="input" style={{ width: 160 }} value={shopId} onChange={e => setShopId(Number(e.target.value))}>{shops.map(s => <option key={s.id} value={s.id}>{s.name}</option>)}</select>}
        <button className="btn btn-secondary" disabled={busy || !shopId} onClick={recompute}><RefreshCw />重算</button>
      </Topbar>
      <div className="scroll"><main className="content" style={{ maxWidth: 1080 }}>
        <Tabs value={status} onChange={setStatus} items={[{ key: 'open', label: '待处理' }, { key: 'done', label: '已处理' }, { key: 'ignored', label: '已忽略' }, { key: 'expired', label: '已失效' }]} />
        {!rows ? <Loading /> : !rows.length ? <section className="panel"><Empty title={status === 'open' ? '没有待处理的建议' : '这里是空的'} desc="导入生意参谋、万相台数据后自动重算。也可以点右上角「重算」。" /></section> : [...groups.entries()].map(([k, list]) => (
          <section key={k} className="panel">
            <div className="panel-head">
              <div className="titles"><div className="panel-title">{list[0].product_name || (k === 'shop' ? '全店' : `上架版本 #${k}`)}</div><div className="panel-sub">{list.length} 条建议</div></div>
              {list[0].product_id && <Link className="btn btn-ghost btn-sm" to={`/products/${list[0].product_id}`}><ExternalLink />商品</Link>}
            </div>
            <div className="mk-advice">{list.map(r => <AdviceCard key={r.id} r={r} canTask={can('task.view')} onTask={() => setTask(r)} onPatch={b => patch(r, b)} />)}</div>
          </section>
        ))}
      </main></div>
      {task && (
        <NewTaskDrawer target={task.listing_id ? { type: 'listing', id: task.listing_id, label: task.product_name } : null}
          initial={{ title: `${task.product_name ? task.product_name + '：' : ''}${RULE[task.rule] || task.rule}${task.keyword ? `「${task.keyword}」` : ''}`, body: ['事实：', ...task.facts.map(f => '- ' + f), ...(task.guesses.length ? ['推测：', ...task.guesses.map(f => '- ' + f)] : []), ...(task.explain ? ['', task.explain] : [])].join('\n') }}
          onClose={() => setTask(null)} onCreated={t => { patch(task, { task_id: t.id }); setTask(null) }} />
      )}
    </>
  )
}

function AdviceCard({ r, canTask, onTask, onPatch }: { r: Row; canTask: boolean; onTask: () => void; onPatch: (b: { status?: 'done' | 'ignored' }) => void }) {
  const toast = useToast()
  const [explain, setExplain] = useState(r.explain)
  const [busy, setBusy] = useState(false)
  const lv = LEVEL[r.level] || LEVEL.info
  const ask = async () => {
    setBusy(true)
    try { setExplain((await marketApi.explain(r.id)).explain) } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const col = (title: string, items: string[], empty: string) => (
    <div className="mk-col"><div className="fs-12 t3">{title}</div>{items.length ? <ul>{items.map((x, i) => <li key={i}>{x}</li>)}</ul> : <div className="fs-12 t4">{empty}</div>}</div>
  )
  return (
    <div className="mk-card">
      <div className="row" style={{ gap: 8 }}>
        <Pill kind={lv.pill}>{lv.label}</Pill>
        <span className="cell-title">{RULE[r.rule] || r.rule}{r.keyword && <span className="t2">「{r.keyword}」</span>}</span>
        <span className="grow" />
        <span className="fs-12 t4">{fmtAgo(r.updated_at)}</span>
      </div>
      <div className="mk-cols">
        {col('事实', r.facts, '—')}
        {col('推测', r.guesses, '没有推测')}
        {col('缺什么数据', r.missing, '数据齐全')}
      </div>
      {explain && <div className="mk-explain fs-13">{explain}</div>}
      {r.hero_b_item_id && <div className="fs-12 t3">这款有 AI 出的首图 B 版，可以在商品的「AI 出图」里换上。</div>}
      {r.status === 'open' && (
        <div className="row" style={{ gap: 6 }}>
          {!explain && <button className="btn btn-ghost btn-sm" disabled={busy} onClick={ask}><MessageSquareText />{busy ? '解释中…' : 'AI 解释'}</button>}
          {canTask && (r.task_id ? <Link className="btn btn-ghost btn-sm" to={`/tasks/${r.task_id}`}>已建任务 #{r.task_id}</Link> : <button className="btn btn-ghost btn-sm" onClick={onTask}><ListPlus />建任务</button>)}
          <span className="grow" />
          <button className="btn btn-secondary btn-sm" onClick={() => onPatch({ status: 'done' })}><Check />已处理</button>
          <button className="btn btn-ghost btn-sm" onClick={() => onPatch({ status: 'ignored' })}><X />忽略</button>
        </div>
      )}
    </div>
  )
}
