import { Fragment, useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useSearchParams } from 'react-router-dom'
import { FileText, Tags, Mic, Search, Package, Check, Sparkles, ShieldCheck, Pencil, Plus, Upload, Trash2, GitCompare, Loader2, X } from 'lucide-react'
import { api, qs, type Page } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { useJobs, jobRunning } from '../lib/jobs'
import { useListingRefs, type ListingRef } from '../lib/listings'
import type { CopyVersion, Issue, Keyword } from '../lib/types'
import { Topbar, Pill, Loading, Empty, Drawer, Field } from '../components/ui'
import { fmtAgo, fmtDate } from '../lib/format'

type Mode = 'versions' | 'keywords' | 'voice'
type Obj = Record<string, unknown>

const SOURCE: Record<string, { label: string; pill: string }> = {
  pipeline: { label: '一键出图', pill: 'pill-neutral' },
  ai_generate: { label: 'AI 生成', pill: 'pill-purple' },
  ai_rewrite: { label: 'AI 改写', pill: 'pill-info' },
  manual: { label: '手动', pill: 'pill-teal' },
  restore: { label: '恢复', pill: 'pill-neutral' }
}

// 文案里给人看的字段，按页面上出现的顺序分组。路径和后端 JSON 路径一致，合规检查的 field 也用它
const SECTIONS: { title: string; rows: [string, string][] }[] = [
  { title: '标题', rows: [['meta.title', '主标题'], ['meta.title_alts', '备选标题']] },
  { title: '卖点', rows: [['content.series', '系列'], ['content.name', '款名'], ['content.nameEn', '英文名'], ['content.oneLiner', '一句话'], ['content.tags', '标签']] },
  {
    title: '详情页', rows: [
      ['content.real.title', '对比标题'], ['content.real.lead', '对比引言'], ['content.real.rows', '对比表'], ['content.real.note', '对比说明'],
      ['content.detailsLead', '细节引言'], ['content.details', '细节'], ['content.wear.lead', '上耳说明'],
      ['content.sizeLead', '尺寸引言'], ['content.params', '参数'], ['content.sizeTip', '尺寸提示'],
      ['content.relatedLead', '搭配引言'], ['content.related', '搭配'], ['content.pack.lead', '包装'], ['content.pack.items', '包装内容'], ['content.care', '保养']
    ]
  },
  { title: '主图', rows: [['main.imgs', '主图文字']] },
  { title: '上架信息', rows: [['meta.attrs', '属性'], ['meta.sku', 'SKU']] }
]
const ROW_LABEL = new Map(SECTIONS.flatMap(s => s.rows))
// 照片字段只是文件名和拍摄提示，不算文案
const SKIP_KEYS = new Set(['f', 'need', 'stamp', 'ai', 'photo', 'photos', 'big', 'a', 'b', 'compliance'])

const normPath = (p: string) => p.replace(/\[(\d+)\]/g, '.$1')
function fieldLabel(path: string) {
  const p = normPath(path)
  let best = ''
  for (const k of ROW_LABEL.keys()) if ((p === k || p.startsWith(k + '.')) && k.length > best.length) best = k
  return best ? ROW_LABEL.get(best)! : path
}
function getPath(o: Obj, path: string): unknown {
  return path.split('.').reduce<unknown>((v, k) => (v && typeof v === 'object' ? (v as Obj)[k] : undefined), o)
}
const isEmpty = (v: unknown) => v === undefined || v === null || v === '' || (Array.isArray(v) && v.length === 0)

export function CopyCenter() {
  const [sp, setSp] = useSearchParams()
  const mode = (sp.get('mode') as Mode) || 'versions'
  const listingId = Number(sp.get('listing')) || 0
  const refs = useListingRefs()
  const { me } = useAuth()
  const [q, setQ] = useState('')
  const shopName = (id: number) => me?.shops.find(s => s.id === id)?.name || `店铺 ${id}`
  const set = (o: Record<string, string>) => {
    const n = new URLSearchParams(sp)
    for (const [k, v] of Object.entries(o)) v ? n.set(k, v) : n.delete(k)
    setSp(n, { replace: true })
  }
  const shown = (refs || []).filter(r => !q || r.product.name.includes(q.trim()))
  const current = refs?.find(r => r.id === listingId)

  return (
    <>
      <Topbar crumbs={['文案中心', ...(mode === 'versions' && current ? [`${current.product.name} · ${shopName(current.shop_id)}`] : mode === 'keywords' ? ['关键词库'] : mode === 'voice' ? ['品牌口吻'] : [])]} />
      <div className="lib">
        <aside className="sidebar lib-filters">
          <div className="nav-group">
            <a className={'nav-item' + (mode === 'versions' ? ' active' : '')} onClick={() => set({ mode: '' })}><FileText />文案版本</a>
            <a className={'nav-item' + (mode === 'keywords' ? ' active' : '')} onClick={() => set({ mode: 'keywords' })}><Tags />关键词库</a>
            <a className={'nav-item' + (mode === 'voice' ? ' active' : '')} onClick={() => set({ mode: 'voice' })}><Mic />品牌口吻</a>
          </div>
          {mode === 'versions' && (
            <div className="nav-group" style={{ minHeight: 0, flex: 1 }}>
              <div className="nav-label">选一款<span className="end">{refs ? shown.length : ''}</span></div>
              <label className="field" style={{ marginBottom: 6 }}><Search /><input placeholder="搜款名" value={q} onChange={e => setQ(e.target.value)} /></label>
              <div className="proc-list stack pick-list">
                {!refs ? <div className="t3 fs-12" style={{ padding: 8 }}>加载中…</div> : shown.length === 0 ? <div className="t3 fs-12" style={{ padding: 8 }}>{q ? '没有符合的款' : '还没有店铺版本'}</div> : shown.map(r => (
                  <button key={r.id} className={'proc-item' + (r.id === listingId ? ' active' : '')} onClick={() => set({ listing: String(r.id) })}>
                    {r.product.cover_url ? <img src={r.product.cover_url} alt="" /> : <span className="cover"><Package /></span>}
                    <span style={{ minWidth: 0, flex: 1 }}>
                      <span className="cell-title ellipsis" style={{ display: 'block' }}>{r.product.name}</span>
                      <span className="cell-sub ellipsis" style={{ display: 'block' }}>{shopName(r.shop_id)}</span>
                    </span>
                  </button>
                ))}
              </div>
            </div>
          )}
        </aside>
        {mode === 'versions' && (current ? <VersionsView key={current.id} ref_={current} /> : (
          <div className="lib-main"><Empty title={refs && refs.length === 0 ? '还没有店铺版本' : '从左边选一款'} desc="每个店铺版本有自己的文案版本记录：一键出图、AI 生成、改写、手动修改都会存一版，选好再应用。" /></div>
        ))}
        {mode === 'keywords' && <KeywordsView />}
        {mode === 'voice' && <VoiceView />}
      </div>
    </>
  )
}

// ———————————————————— 文案版本 ————————————————————

function VersionsView({ ref_ }: { ref_: ListingRef }) {
  const { can } = useAuth()
  const toast = useToast()
  const editable = can('copy.edit')
  const [list, setList] = useState<{ items: CopyVersion[]; current: number | null } | null>(null)
  const [sel, setSel] = useState(0)
  const [detail, setDetail] = useState<CopyVersion | null>(null)
  const [cmpId, setCmpId] = useState(0)
  const [cmp, setCmp] = useState<CopyVersion | null>(null)
  const [issues, setIssues] = useState<Issue[] | null>(null)
  const [checking, setChecking] = useState(false)
  const [withAI, setWithAI] = useState(true)
  const [editing, setEditing] = useState(false)
  const [jobIds, setJobIds] = useState<number[]>([])
  const jobs = useJobs(jobIds)
  const handled = useRef(new Set<number>())

  const load = useCallback(async (select?: number) => {
    try {
      const r = await api<Page<CopyVersion> & { current: number | null }>(`/api/listings/${ref_.id}/copies?page_size=200`)
      setList({ items: r.items, current: r.current })
      setSel(s => select || (r.items.some(v => v.id === s) ? s : r.current || r.items[0]?.id || 0))
    } catch (e) { toast(errText(e), 'error'); setList({ items: [], current: null }) }
  }, [ref_.id, toast])
  useEffect(() => { load() }, [load])

  useEffect(() => {
    setIssues(null)
    if (!sel) { setDetail(null); return }
    let stop = false
    api<CopyVersion>(`/api/copies/${sel}`).then(v => !stop && setDetail(v)).catch(e => toast(errText(e), 'error'))
    return () => { stop = true }
  }, [sel, toast])
  useEffect(() => {
    if (!cmpId) { setCmp(null); return }
    api<CopyVersion>(`/api/copies/${cmpId}`).then(setCmp).catch(e => toast(errText(e), 'error'))
  }, [cmpId, toast])

  // AI 生成 / 改写跑完：刷新列表并选中新版本
  useEffect(() => {
    for (const j of Object.values(jobs)) {
      if (jobRunning(j) || handled.current.has(j.id)) continue
      handled.current.add(j.id)
      if (j.status === 'done') {
        const cid = Number((j.result as { copy_id?: number } | null)?.copy_id) || undefined
        toast(j.kind === 'copy.rewrite' ? '改写好了，存成了新版本' : '生成好了，存成了新版本', 'ok')
        load(cid)
      } else toast(j.message || 'AI 任务失败', 'error')
    }
  }, [jobs, load, toast])
  const running = Object.values(jobs).find(jobRunning) || (jobIds.length > 0 && !jobs[jobIds[jobIds.length - 1]] ? { progress: 0, message: '排队中' } : null)

  const apply = async () => {
    if (!detail) return
    try { await api(`/api/copies/${detail.id}/apply`, { method: 'POST' }); toast('已应用，下次出图用这版文案', 'ok'); load() } catch (e) { toast(errText(e), 'error') }
  }
  const check = async () => {
    if (!detail) return
    setChecking(true)
    try {
      const r = await api<{ issues: Issue[] }>(`/api/copies/${detail.id}/check` + qs({ ai: withAI ? 1 : 0 }), { method: 'POST' })
      setIssues(r.issues || [])
      if (!r.issues?.length) toast('没查出问题', 'ok')
    } catch (e) { toast(errText(e), 'error') } finally { setChecking(false) }
  }
  const startJob = async (path: string, body: unknown) => {
    try {
      const r = await api<{ job: { id: number } }>(path, { body })
      setJobIds(ids => [...ids, r.job.id])
    } catch (e) { toast(errText(e), 'error') }
  }

  if (!list) return <div className="lib-main"><Loading /></div>
  const isCurrent = !!detail && detail.id === list.current
  const others = list.items.filter(v => v.id !== sel)

  return (
    <>
      <div className="lib-main">
        <div className="lib-toolbar">
          {detail ? <>
            <span className="cell-title mono">#{detail.id}</span>
            <Pill kind={(SOURCE[detail.source] || SOURCE.manual).pill} dot={false}>{(SOURCE[detail.source] || { label: detail.source }).label}</Pill>
            {isCurrent ? <Pill kind="pill-success">当前在用</Pill> : <span className="t3 fs-12">未在用</span>}
            <span className="t4 fs-12">{fmtDate(detail.created_at, true)}{detail.note ? ` · ${detail.note}` : ''}</span>
          </> : <span className="t3 fs-12">还没有文案版本</span>}
          <span className="spacer" />
          {detail && others.length > 0 && (
            <label className="inline fs-12 t3"><GitCompare size={14} />对比
              <select className="input" style={{ width: 170 }} value={cmpId} onChange={e => setCmpId(Number(e.target.value))}>
                <option value={0}>不对比</option>
                {others.map(v => <option key={v.id} value={v.id}>#{v.id} {(SOURCE[v.source] || { label: v.source }).label} · {fmtAgo(v.created_at)}</option>)}
              </select>
            </label>
          )}
          {detail && editable && <button className="btn btn-secondary btn-sm" onClick={() => setEditing(true)}><Pencil />改一改</button>}
        </div>
        <div className="scroll"><div className="copy-body">
          {list.items.length === 0 ? (
            <Empty title="这款还没有文案" desc={editable ? '在店铺版本里跑一次一键出图，或者在右边用 AI 生成一版。' : '在店铺版本里跑一次一键出图后，这里会自动存一版。'} />
          ) : !detail ? <Loading /> : cmp ? <DiffView a={cmp} b={detail} /> : <>
            {issues && issues.length > 0 && <IssueList issues={issues} onClose={() => setIssues(null)} />}
            <CopyDoc v={detail} issues={issues || []} />
          </>}
        </div></div>
      </div>

      <aside className="inspector lib-inspector">
        <div className="inspector-body">
          <div className="inspector-section">
            <div className="head"><span className="sub-title">版本</span><span className="t4 fs-12">{list.items.length} 个</span></div>
            <div className="ver-list">
              {list.items.map(v => {
                const s = SOURCE[v.source] || { label: v.source, pill: 'pill-neutral' }
                return (
                  <button key={v.id} className={'ver-row' + (v.id === sel ? ' active' : '')} onClick={() => { setSel(v.id); if (cmpId === v.id) setCmpId(0) }}>
                    <span className="mono t3">#{v.id}</span>
                    <span className="ellipsis grow">{v.title || '（无标题）'}</span>
                    {v.id === list.current ? <Check className="ok" /> : null}
                    <span className="meta"><Pill kind={s.pill} dot={false}>{s.label}</Pill><span className="t4">{fmtAgo(v.created_at)}</span></span>
                  </button>
                )
              })}
            </div>
          </div>
          {detail && (
            <div className="inspector-section">
              <div className="head"><span className="sub-title">合规检查</span></div>
              <label className="setting fs-12"><span className="t2">同时让 AI 审查</span><input type="checkbox" className="switch" checked={withAI} onChange={e => setWithAI(e.target.checked)} /></label>
              <button className="btn btn-secondary btn-block" disabled={checking} onClick={check}>{checking ? <Loader2 className="spin" /> : <ShieldCheck />}{checking ? '检查中…' : '检查这一版'}</button>
              {issues && <span className="fs-12 t3">{issues.length ? `${issues.filter(i => i.level === 'error').length} 处必须改，${issues.filter(i => i.level === 'warn').length} 处建议看一眼，已在原文标出` : '没查出问题'}</span>}
            </div>
          )}
          {editable && <AIPanel disabled={!!running} hasVersion={!!detail}
            onRewrite={(scope, instructions) => detail && startJob(`/api/copies/${detail.id}/rewrite`, { scope, instructions })}
            onGenerate={instructions => startJob(`/api/listings/${ref_.id}/copies/generate`, { instructions })} />}
          {running && (
            <div className="inspector-section">
              <div className="row between fs-12"><span className="inline"><Loader2 className="spin" size={14} />{running.message || 'AI 在写…'}</span><span className="mono">{running.progress}%</span></div>
              <div className="progress"><span style={{ width: `${running.progress}%` }} /></div>
            </div>
          )}
        </div>
        {editable && detail && (
          <div className="inspector-foot">
            <button className="btn btn-primary btn-block" disabled={isCurrent} onClick={apply}><Check />{isCurrent ? '这一版正在用' : '应用这一版'}</button>
            <div className="fs-12 t4" style={{ marginTop: 6 }}>应用后写进店铺版本，重新出图才会用上。</div>
          </div>
        )}
      </aside>
      {editing && detail && <EditDrawer v={detail} listingId={ref_.id} onClose={() => setEditing(false)} onSaved={id => { setEditing(false); load(id) }} />}
    </>
  )
}

const QUICK_ASK = ['更口语一点', '更简洁', '突出送礼', '突出真珍珠', '少用形容词']

function AIPanel({ disabled, hasVersion, onRewrite, onGenerate }: { disabled: boolean; hasVersion: boolean; onRewrite: (scope: string, ins: string) => void; onGenerate: (ins: string) => void }) {
  const [tab, setTab] = useState<'rewrite' | 'generate'>(hasVersion ? 'rewrite' : 'generate')
  const [scope, setScope] = useState('title')
  const [ins, setIns] = useState('')
  const add = (t: string) => setIns(v => (v.includes(t) ? v : v ? `${v}，${t}` : t))
  const rw = tab === 'rewrite' && hasVersion
  return (
    <div className="inspector-section">
      <div className="head"><span className="sub-title">AI 写文案</span></div>
      <div className="segmented block">
        <button className={rw ? 'active' : ''} disabled={!hasVersion} onClick={() => setTab('rewrite')}>改写这一版</button>
        <button className={rw ? '' : 'active'} onClick={() => setTab('generate')}>重新生成</button>
      </div>
      {rw && (
        <div className="proc-row"><span className="k">改哪里</span>
          <div className="segmented">{[['title', '标题'], ['selling', '卖点'], ['all', '全部']].map(([k, t]) => <button key={k} className={scope === k ? 'active' : ''} onClick={() => setScope(k)}>{t}</button>)}</div>
        </div>
      )}
      <textarea className="textarea" rows={3} maxLength={1000} value={ins} onChange={e => setIns(e.target.value)}
        placeholder={rw ? '想怎么改，如：标题加上「通勤」，别用「高级感」' : '附加要求，可以不写。会自动带上品牌口吻和关键词库'} />
      <div className="inline">{QUICK_ASK.map(t => <button key={t} className="chip outline" onClick={() => add(t)}>{t}</button>)}</div>
      <button className="btn btn-secondary btn-block" disabled={disabled || (rw && !ins.trim())} onClick={() => (rw ? onRewrite(scope, ins.trim()) : onGenerate(ins.trim()))}>
        <Sparkles />{rw ? '改写，存成新版本' : '生成一版新文案'}
      </button>
      <span className="fs-12 t4">{rw ? '只改选中的部分，其他字段原样保留。' : '结果存成新版本，不会自动应用。'}</span>
    </div>
  )
}

// 把文案按分组摊开显示；合规问题的原文片段在对应位置标出来
function CopyDoc({ v, issues }: { v: CopyVersion; issues: Issue[] }) {
  const root: Obj = { content: v.content || {}, main: v.main || {}, meta: v.meta || {} }
  return (
    <div className="stack" style={{ gap: 12 }}>
      {SECTIONS.map(s => {
        const rows = s.rows.filter(([p]) => !isEmpty(getPath(root, p)))
        if (!rows.length) return null
        return (
          <section key={s.title} className="panel">
            <div className="panel-head"><div className="panel-title">{s.title}</div></div>
            <div className="panel-pad"><dl className="kv copy-kv">
              {rows.map(([p, label]) => {
                const mine = issues.filter(i => normPath(i.field).startsWith(p))
                return (
                  <Fragment key={p}>
                    <dt>{label}{mine.length > 0 && <span className={'issue-dot ' + (mine.some(i => i.level === 'error') ? 'error' : 'warn')} />}</dt>
                    <dd><Value v={getPath(root, p)} issues={issues} /></dd>
                  </Fragment>
                )
              })}
            </dl></div>
          </section>
        )
      })}
    </div>
  )
}

function Value({ v, issues }: { v: unknown; issues: Issue[] }): ReactNode {
  if (typeof v === 'string' || typeof v === 'number') return <span className="pre">{mark(String(v), issues)}</span>
  if (Array.isArray(v)) {
    if (v.every(x => typeof x === 'string')) return v.length <= 8 && v.every(x => (x as string).length <= 16)
      ? <span className="inline">{v.map((x, i) => <span key={i} className="tag tag-gray">{mark(x as string, issues)}</span>)}</span>
      : <ol className="copy-lines">{v.map((x, i) => <li key={i}>{mark(x as string, issues)}</li>)}</ol>
    if (v.every(x => Array.isArray(x))) return (
      <table className="mini-table"><tbody>{v.map((r, i) => <tr key={i}>{(r as unknown[]).map((c, j) => <td key={j} className={j === 0 ? 't3' : ''}>{mark(String(c ?? ''), issues)}</td>)}</tr>)}</tbody></table>
    )
    return <ol className="copy-lines">{v.map((x, i) => <li key={i}>{x && typeof x === 'object' ? <ObjLine o={x as Obj} issues={issues} /> : String(x)}</li>)}</ol>
  }
  if (v && typeof v === 'object') return <ObjLine o={v as Obj} issues={issues} />
  return null
}

const OBJ_LABEL: Record<string, string> = { n: '第', layout: '版式', label: '角标', title: '标题', name: '名称', text: '文字', t: '文字', size: '尺寸', mm: '尺寸', cap: '说明', price: '价格' }
function ObjLine({ o, issues }: { o: Obj; issues: Issue[] }) {
  const parts = Object.entries(o).filter(([k, x]) => !SKIP_KEYS.has(k) && !isEmpty(x) && (typeof x !== 'object' || Array.isArray(x)))
  return (
    <span className="obj-line">{parts.map(([k, x]) => (
      <span key={k}><span className="t4">{k === 'n' ? `第 ${x} 张` : OBJ_LABEL[k] || k}</span>{k !== 'n' && <>{' '}{Array.isArray(x) ? (x as unknown[]).map(String).map((s, i) => <Fragment key={i}>{i > 0 && ' / '}{mark(s, issues)}</Fragment>) : mark(String(x), issues)}</>}</span>
    ))}</span>
  )
}

// 在一段文字里标出合规问题的原文
function mark(text: string, issues: Issue[]): ReactNode {
  const hits = issues.filter(i => i.text && text.includes(i.text))
  if (!hits.length) return text
  const out: ReactNode[] = []
  let rest = text
  let k = 0
  while (rest) {
    let pos = -1
    let hit: Issue | null = null
    for (const h of hits) {
      const p = rest.indexOf(h.text)
      if (p >= 0 && (pos < 0 || p < pos || (p === pos && h.text.length > hit!.text.length))) { pos = p; hit = h }
    }
    if (!hit) { out.push(rest); break }
    if (pos > 0) out.push(rest.slice(0, pos))
    out.push(<mark key={k++} className={'hl ' + hit.level} title={hit.reason + (hit.suggest ? `\n建议：${hit.suggest}` : '')}>{hit.text}</mark>)
    rest = rest.slice(pos + hit.text.length)
  }
  return out
}

function IssueList({ issues, onClose }: { issues: Issue[]; onClose: () => void }) {
  const sorted = [...issues].sort((a, b) => (a.level === b.level ? 0 : a.level === 'error' ? -1 : 1))
  return (
    <section className="panel">
      <div className="panel-head"><ShieldCheck className="t3" size={16} /><div className="panel-title">合规检查</div><span className="t3 fs-12">{issues.length} 处</span><span className="grow" /><button className="close-btn" onClick={onClose} aria-label="收起"><X /></button></div>
      <div className="list issue-list">{sorted.map((i, n) => (
        <div key={n} className="list-item">
          <Pill kind={i.level === 'error' ? 'pill-danger' : 'pill-warning'} dot={false}>{i.level === 'error' ? '必须改' : '看一眼'}</Pill>
          <div style={{ minWidth: 0, flex: 1 }}>
            <div className="title">{i.text ? <>「{i.text}」</> : null}<span className="t3" style={{ fontWeight: 400 }}>{i.reason}</span></div>
            <div className="meta">{fieldLabel(i.field)}{i.suggest ? <> · 建议：<span className="t2">{i.suggest}</span></> : null}</div>
          </div>
        </div>
      ))}</div>
    </section>
  )
}

// 两个版本逐字段对比：只列有变化的字段
function flatten(v: unknown, prefix: string, out: Map<string, string>) {
  if (v === null || v === undefined) return
  if (typeof v !== 'object') { if (String(v) !== '') out.set(prefix, String(v)); return }
  if (Array.isArray(v) && v.every(x => typeof x !== 'object' || Array.isArray(x))) {
    const s = v.map(x => (Array.isArray(x) ? x.join('：') : String(x))).join('\n')
    if (s) out.set(prefix, s)
    return
  }
  for (const [k, x] of Object.entries(v as Obj)) if (!SKIP_KEYS.has(k)) flatten(x, prefix ? `${prefix}.${k}` : k, out)
}
function DiffView({ a, b }: { a: CopyVersion; b: CopyVersion }) {
  const rows = useMemo(() => {
    const ma = new Map<string, string>(), mb = new Map<string, string>()
    flatten({ meta: a.meta, content: a.content, main: a.main }, '', ma)
    flatten({ meta: b.meta, content: b.content, main: b.main }, '', mb)
    const keys = [...new Set([...mb.keys(), ...ma.keys()])]
    return keys.filter(k => ma.get(k) !== mb.get(k)).map(k => ({ k, a: ma.get(k) || '', b: mb.get(k) || '' }))
  }, [a, b])
  return (
    <section className="panel">
      <div className="panel-head"><GitCompare className="t3" size={16} /><div className="panel-title">#{a.id} → #{b.id}</div><span className="t3 fs-12">{rows.length ? `${rows.length} 处不同` : '内容完全一样'}</span></div>
      {rows.length > 0 && (
        <div className="table-wrap"><table className="table diff-table">
          <thead><tr><th style={{ width: 120 }}>字段</th><th>#{a.id}（{(SOURCE[a.source] || { label: a.source }).label}）</th><th>#{b.id}（{(SOURCE[b.source] || { label: b.source }).label}）</th></tr></thead>
          <tbody>{rows.map(r => (
            <tr key={r.k}><td className="t3 fs-12" title={r.k}>{fieldLabel(r.k)}</td><td className="pre del">{r.a || <span className="t4">（空）</span>}</td><td className="pre ins">{r.b || <span className="t4">（空）</span>}</td></tr>
          ))}</tbody>
        </table></div>
      )}
    </section>
  )
}

// 手动改常用的文字字段，存成新版本
const EDIT_FIELDS: { path: string; label: string; list?: boolean; rows?: number }[] = [
  { path: 'meta.title', label: '主标题', rows: 2 },
  { path: 'meta.title_alts', label: '备选标题（一行一个）', list: true, rows: 2 },
  { path: 'content.name', label: '款名', rows: 1 },
  { path: 'content.oneLiner', label: '一句话', rows: 3 },
  { path: 'content.tags', label: '标签（一行一个）', list: true, rows: 3 },
  { path: 'content.real.note', label: '对比说明', rows: 2 },
  { path: 'content.detailsLead', label: '细节引言', rows: 2 },
  { path: 'content.wear.lead', label: '上耳说明', rows: 2 },
  { path: 'content.sizeLead', label: '尺寸引言', rows: 2 },
  { path: 'content.relatedLead', label: '搭配引言', rows: 2 }
]
function EditDrawer({ v, listingId, onClose, onSaved }: { v: CopyVersion; listingId: number; onClose: () => void; onSaved: (id: number) => void }) {
  const toast = useToast()
  const root: Obj = { content: v.content || {}, main: v.main || {}, meta: v.meta || {} }
  const fields = EDIT_FIELDS.filter(f => f.path === 'meta.title' || getPath(root, f.path) !== undefined)
  const [vals, setVals] = useState<Record<string, string>>(() => Object.fromEntries(fields.map(f => {
    const x = getPath(root, f.path)
    return [f.path, Array.isArray(x) ? x.join('\n') : String(x ?? '')]
  })))
  const [note, setNote] = useState('')
  const [andApply, setAndApply] = useState(false)
  const [busy, setBusy] = useState(false)
  const save = async () => {
    const next = JSON.parse(JSON.stringify(root)) as Obj
    for (const f of fields) {
      const keys = f.path.split('.')
      let o = next
      for (const k of keys.slice(0, -1)) { if (!o[k] || typeof o[k] !== 'object') o[k] = {}; o = o[k] as Obj }
      const raw = vals[f.path] ?? ''
      o[keys[keys.length - 1]] = f.list ? raw.split('\n').map(s => s.trim()).filter(Boolean) : raw.trim()
    }
    setBusy(true)
    try {
      const r = await api<CopyVersion>(`/api/listings/${listingId}/copies`, { body: { title: vals['meta.title']?.trim(), content: next.content, main: next.main, meta: next.meta, note: note.trim() || `改自 #${v.id}` } })
      if (andApply) await api(`/api/copies/${r.id}/apply`, { method: 'POST' })
      toast(andApply ? '已存成新版本并应用' : '已存成新版本', 'ok')
      onSaved(r.id)
    } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  return (
    <Drawer title={`改 #${v.id}，存成新版本`} width={480} onClose={onClose} foot={<>
      <label className="inline fs-12 t2" style={{ marginRight: 'auto' }}><input type="checkbox" className="check" checked={andApply} onChange={e => setAndApply(e.target.checked)} />存完直接应用</label>
      <button className="btn btn-secondary" onClick={onClose}>取消</button>
      <button className="btn btn-primary" disabled={busy} onClick={save}>保存</button>
    </>}>
      {fields.map(f => (
        <Field key={f.path} label={f.label}>
          {f.rows === 1 ? <input className="input" value={vals[f.path]} onChange={e => setVals(s => ({ ...s, [f.path]: e.target.value }))} />
            : <textarea className="textarea" rows={f.rows} value={vals[f.path]} onChange={e => setVals(s => ({ ...s, [f.path]: e.target.value }))} />}
        </Field>
      ))}
      <Field label="版本备注" hint="其他字段（参数、细节、主图文字）沿用原版本。"><input className="input" value={note} placeholder={`改自 #${v.id}`} onChange={e => setNote(e.target.value)} /></Field>
    </Drawer>
  )
}

// ———————————————————— 关键词库 ————————————————————

const GROUPS = ['品类词', '属性词', '场景词', '禁用词']
const grpTag = (g: string) => (g === '禁用词' ? 'tag-red' : g === '品类词' ? 'tag-blue' : g === '属性词' ? 'tag-green' : g === '场景词' ? 'tag-purple' : 'tag-gray')

function KeywordsView() {
  const { me, can } = useAuth()
  const toast = useToast()
  const editable = can('copy.edit')
  const shops = me?.shops || []
  const [shop, setShop] = useState('')
  const [grp, setGrp] = useState('')
  const [q, setQ] = useState('')
  const [items, setItems] = useState<Keyword[] | null>(null)
  const [edit, setEdit] = useState<Keyword | 'new' | null>(null)
  const [importing, setImporting] = useState(false)
  const load = useCallback(() => {
    api<Page<Keyword>>('/api/keywords' + qs({ shop_id: shop, page_size: 1000 })).then(r => setItems(r.items)).catch(e => { toast(errText(e), 'error'); setItems([]) })
  }, [shop, toast])
  useEffect(() => { load() }, [load])
  const groups = useMemo(() => [...new Set([...GROUPS, ...(items || []).map(k => k.grp).filter(Boolean)])], [items])
  const shown = (items || []).filter(k => (!grp || k.grp === grp) && (!q || k.word.includes(q.trim()) || k.note.includes(q.trim())))
  const shopName = (id: number | null) => (id ? shops.find(s => s.id === id)?.name || `店铺 ${id}` : '全店通用')
  const remove = async (k: Keyword) => {
    if (!confirm(`删除关键词「${k.word}」？`)) return
    try { await api(`/api/keywords/${k.id}`, { method: 'DELETE' }); load() } catch (e) { toast(errText(e), 'error') }
  }
  return (
    <div className="lib-main">
      <div className="lib-toolbar">
        <select className="input" style={{ width: 150 }} value={shop} onChange={e => setShop(e.target.value)}>
          <option value="">全部店铺</option>
          <option value="0">只看全店通用</option>
          {shops.map(s => <option key={s.id} value={s.id}>{s.name}（含通用）</option>)}
        </select>
        <div className="segmented">
          <button className={grp ? '' : 'active'} onClick={() => setGrp('')}>全部</button>
          {groups.map(g => <button key={g} className={grp === g ? 'active' : ''} onClick={() => setGrp(g)}>{g}</button>)}
        </div>
        <label className="field" style={{ width: 200 }}><Search /><input placeholder="搜关键词、备注" value={q} onChange={e => setQ(e.target.value)} /></label>
        <span className="spacer" />
        {editable && <>
          <button className="btn btn-secondary btn-sm" onClick={() => setImporting(true)}><Upload />批量导入</button>
          <button className="btn btn-primary btn-sm" onClick={() => setEdit('new')}><Plus />加关键词</button>
        </>}
      </div>
      <div className="scroll"><div className="copy-body">
        <section className="panel">
          {!items ? <Loading /> : shown.length === 0 ? (
            <Empty title={items.length ? '没有符合的关键词' : '关键词库是空的'} desc="AI 写文案时会优先用权重高的词；分到「禁用词」的词，合规检查会标红。" />
          ) : (
            <div className="table-wrap"><table className="table">
              <thead><tr><th>关键词</th><th>分组</th><th className="num">权重</th><th>适用</th><th>备注</th>{editable && <th style={{ width: 90 }} />}</tr></thead>
              <tbody>{shown.map(k => (
                <tr key={k.id}>
                  <td className="cell-title">{k.word}</td>
                  <td>{k.grp ? <span className={'tag ' + grpTag(k.grp)}>{k.grp}</span> : <span className="t4">—</span>}</td>
                  <td className="num mono">{k.weight}</td>
                  <td className="t2">{shopName(k.shop_id)}</td>
                  <td className="t3 ellipsis" style={{ maxWidth: 260 }}>{k.note || '—'}</td>
                  {editable && <td className="num"><span className="inline">
                    <button className="btn btn-ghost btn-icon btn-sm" title="编辑" onClick={() => setEdit(k)}><Pencil /></button>
                    <button className="btn btn-ghost btn-icon btn-sm" title="删除" onClick={() => remove(k)}><Trash2 /></button>
                  </span></td>}
                </tr>
              ))}</tbody>
            </table></div>
          )}
        </section>
        {items && items.length > 0 && <div className="fs-12 t4">共 {items.length} 个，AI 写文案时取权重前 50 个，禁用词单独列给它。</div>}
      </div></div>
      {edit && <KeywordDrawer kw={edit === 'new' ? null : edit} groups={groups} defShop={shop && shop !== '0' ? Number(shop) : null} onClose={() => setEdit(null)} onDone={() => { setEdit(null); load() }} />}
      {importing && <ImportDrawer defShop={shop && shop !== '0' ? Number(shop) : null} onClose={() => setImporting(false)} onDone={() => { setImporting(false); load() }} />}
    </div>
  )
}

function ShopSelect({ value, onChange }: { value: number | null; onChange: (v: number | null) => void }) {
  const { me } = useAuth()
  return (
    <select className="input" value={value ?? ''} onChange={e => onChange(e.target.value ? Number(e.target.value) : null)}>
      <option value="">全店通用</option>
      {(me?.shops || []).map(s => <option key={s.id} value={s.id}>只用于 {s.name}</option>)}
    </select>
  )
}

function KeywordDrawer({ kw, groups, defShop, onClose, onDone }: { kw: Keyword | null; groups: string[]; defShop: number | null; onClose: () => void; onDone: () => void }) {
  const toast = useToast()
  const [word, setWord] = useState(kw?.word || '')
  const [grp, setGrp] = useState(kw?.grp || '')
  const [weight, setWeight] = useState(String(kw?.weight ?? 10))
  const [note, setNote] = useState(kw?.note || '')
  const [shopId, setShopId] = useState<number | null>(kw ? kw.shop_id : defShop)
  const [busy, setBusy] = useState(false)
  const save = async () => {
    setBusy(true)
    const body = { word: word.trim(), grp: grp.trim(), weight: Number(weight) || 0, note: note.trim(), shop_id: shopId }
    try {
      if (kw) await api(`/api/keywords/${kw.id}`, { method: 'PATCH', body })
      else await api('/api/keywords', { body })
      toast('已保存', 'ok'); onDone()
    } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  return (
    <Drawer title={kw ? '编辑关键词' : '加关键词'} onClose={onClose} foot={<>
      <button className="btn btn-secondary" onClick={onClose}>取消</button>
      <button className="btn btn-primary" disabled={busy || !word.trim()} onClick={save}>保存</button>
    </>}>
      <Field label="关键词" req><input className="input" autoFocus value={word} maxLength={80} onChange={e => setWord(e.target.value)} placeholder="如：淡水珍珠" /></Field>
      <div className="form-row">
        <Field label="分组">
          <input className="input" list="kw-groups" value={grp} onChange={e => setGrp(e.target.value)} placeholder="选或直接写" />
          <datalist id="kw-groups">{groups.map(g => <option key={g} value={g} />)}</datalist>
        </Field>
        <Field label="权重" hint="越大越优先"><input className="input mono" inputMode="numeric" value={weight} onChange={e => setWeight(e.target.value.replace(/[^\d-]/g, ''))} /></Field>
      </div>
      <Field label="适用店铺"><ShopSelect value={shopId} onChange={setShopId} /></Field>
      <Field label="备注"><input className="input" value={note} onChange={e => setNote(e.target.value)} placeholder="如：平台禁用，用「高品质」代替" /></Field>
    </Drawer>
  )
}

function ImportDrawer({ defShop, onClose, onDone }: { defShop: number | null; onClose: () => void; onDone: () => void }) {
  const toast = useToast()
  const [shopId, setShopId] = useState<number | null>(defShop)
  const [lines, setLines] = useState('')
  const [busy, setBusy] = useState(false)
  const n = lines.split('\n').filter(l => l.trim()).length
  const save = async () => {
    setBusy(true)
    try {
      const r = await api<{ imported: number }>('/api/keywords/import', { body: { shop_id: shopId, lines } })
      toast(`导入了 ${r.imported} 个`, 'ok'); onDone()
    } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  return (
    <Drawer title="批量导入关键词" width={480} onClose={onClose} foot={<>
      <button className="btn btn-secondary" onClick={onClose}>取消</button>
      <button className="btn btn-primary" disabled={busy || !n} onClick={save}>导入 {n || ''} 行</button>
    </>}>
      <Field label="适用店铺"><ShopSelect value={shopId} onChange={setShopId} /></Field>
      <Field label="关键词" hint="一行一个：词、分组、权重用 Tab 隔开，后两项可以不写。从表格里复制两三列直接粘贴就行；已有的词会更新分组和权重。">
        <textarea className="textarea mono" rows={14} value={lines} onChange={e => setLines(e.target.value)} placeholder={'淡水珍珠\t品类词\t30\n通勤\t场景词\t20\n顶级\t禁用词'} />
      </Field>
    </Drawer>
  )
}

// ———————————————————— 品牌口吻 ————————————————————

function VoiceView() {
  const { me, can } = useAuth()
  const toast = useToast()
  const shops = me?.shops || []
  const [shopId, setShopId] = useState(shops[0]?.id || 0)
  const [text, setText] = useState('')
  const [examples, setExamples] = useState('')
  const [at, setAt] = useState('')
  const [loaded, setLoaded] = useState(false)
  const [busy, setBusy] = useState(false)
  const editable = can('copy.edit')
  useEffect(() => {
    if (!shopId) return
    setLoaded(false)
    api<{ text: string; examples: string[]; updated_at: string }>(`/api/shops/${shopId}/voice`)
      .then(r => { setText(r.text || ''); setExamples((r.examples || []).join('\n')); setAt(r.updated_at || ''); setLoaded(true) })
      .catch(e => toast(errText(e), 'error'))
  }, [shopId, toast])
  const save = async () => {
    setBusy(true)
    try {
      const r = await api<{ updated_at: string }>(`/api/shops/${shopId}/voice`, { method: 'PUT', body: { text: text.trim(), examples: examples.split('\n').map(s => s.trim()).filter(Boolean) } })
      setAt(r.updated_at); toast('已保存，之后 AI 写文案会按这个口吻', 'ok')
    } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  if (!shops.length) return <div className="lib-main"><Empty title="你还没有分到店铺" desc="品牌口吻按店铺设置。" /></div>
  return (
    <div className="lib-main">
      <div className="lib-toolbar">
        <select className="input" style={{ width: 180 }} value={shopId} onChange={e => setShopId(Number(e.target.value))}>
          {shops.map(s => <option key={s.id} value={s.id}>{s.name}</option>)}
        </select>
        <span className="t4 fs-12">{at ? `上次修改 ${fmtAgo(at)}` : '还没设置过'}</span>
        <span className="spacer" />
        {editable && <button className="btn btn-primary btn-sm" disabled={busy || !loaded} onClick={save}>保存</button>}
      </div>
      <div className="scroll"><div className="copy-body narrow">
        <section className="panel">
          <div className="panel-head"><div className="titles"><div className="panel-title">口吻说明</div><div className="panel-sub">AI 生成和改写这个店的文案时都会带上。写清楚对谁说话、用什么语气、哪些词不用。</div></div></div>
          <div className="panel-pad">
            <textarea className="textarea" rows={8} readOnly={!editable} value={text} onChange={e => setText(e.target.value)}
              placeholder={'例：\n像懂珍珠的朋友在推荐，平实、具体，不夸张。\n多讲看得见的细节（光泽、尺寸、做工），少用「高级感」「氛围感」这类空词。\n句子短，一句只说一件事。'} />
          </div>
        </section>
        <section className="panel">
          <div className="panel-head"><div className="titles"><div className="panel-title">例句</div><div className="panel-sub">一行一句，挑几句你觉得写得对味的文案给 AI 参考。</div></div></div>
          <div className="panel-pad">
            <textarea className="textarea" rows={6} readOnly={!editable} value={examples} onChange={e => setExamples(e.target.value)}
              placeholder={'一颗真淡水珍珠，配一枚小小的银蝴蝶结。\n通勤日常都能戴，不抢衣服的风头。'} />
          </div>
        </section>
      </div></div>
    </div>
  )
}
