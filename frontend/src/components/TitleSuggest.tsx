import { useCallback, useEffect, useState } from 'react'
import { Sparkles, Loader2, Check, History } from 'lucide-react'
import { api, ApiError } from '../lib/api'
import { useToast, errText } from '../lib/toast'
import { Drawer, Pill, Empty } from './ui'
import { fmtDate, fmtAgo } from '../lib/format'

// 后端 copywriting/titles.go：POST /titles/suggest、/titles/apply，GET /titles/history
type Compliance = { level: 'error' | 'warn'; word: string; msg: string }
export type TitleIdea = {
  title: string; chars: number; over_limit: boolean
  keywords: { word: string; score: number | null }[]
  overlap_bps: number; overlap_with: string
  compliance: Compliance[]; focus: string; note: string
}
type Suggest = {
  current: { title: string; chars: number }
  titles: TitleIdea[]
  change_allowed: boolean; next_change_at: string
  inputs: { keywords: number; competitors: number; search_terms: number; history: number }
}
type HistoryRow = { id: number; old_title: string; new_title: string; reason: string; created_by_name: string; created_at: string }
type HistoryResp = { items: HistoryRow[]; change_allowed: boolean; next_change_at: string }

const FOCUS: Record<string, string> = { 覆盖: '覆盖高价值词', 款式: '偏款式风格', 送礼: '偏送礼场景' }
const LIMIT = 60 // titleLength：汉字算 2

function lockedText(next: string) {
  return next ? `距上次改标题不满 7 天，${fmtDate(next, true)} 以后才能再改` : '暂时不能改标题'
}

export function TitleSuggestDrawer({ listingId, editable, onClose, onApplied }: { listingId: number; editable: boolean; onClose: () => void; onApplied: (copyId?: number) => void }) {
  const toast = useToast()
  const [data, setData] = useState<Suggest | null>(null)
  const [hist, setHist] = useState<HistoryResp | null>(null)
  const [busy, setBusy] = useState(false)
  const [note, setNote] = useState('')
  const [pick, setPick] = useState(-1)
  const [reason, setReason] = useState('')
  const [applying, setApplying] = useState(false)

  const loadHistory = useCallback(() => {
    api<HistoryResp>(`/api/listings/${listingId}/titles/history`).then(setHist).catch(e => toast(errText(e), 'error'))
  }, [listingId, toast])
  useEffect(() => { loadHistory() }, [loadHistory])

  const run = async () => {
    setBusy(true); setPick(-1)
    try { setData(await api<Suggest>(`/api/listings/${listingId}/titles/suggest`, { body: note.trim() ? { note: note.trim() } : {} })) } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const apply = async () => {
    const t = data?.titles[pick]
    if (!t) return
    setApplying(true)
    try {
      const r = await api<{ copy_id?: number }>(`/api/listings/${listingId}/titles/apply`, { body: { title: t.title, reason: reason.trim() || `标题建议 · ${FOCUS[t.focus] || t.focus || '第 ' + (pick + 1) + ' 个'}` } })
      toast('新标题已用上，改动记进了标题历史', 'ok')
      onApplied(r.copy_id); onClose()
    } catch (e) {
      const d = e instanceof ApiError ? (e.data as { compliance?: Compliance[] } | null) : null
      toast(errText(e) + (d?.compliance?.length ? '：' + d.compliance.map(c => c.word || c.msg).join('、') : ''), 'error')
    } finally { setApplying(false) }
  }

  const allowed = data ? data.change_allowed : hist ? hist.change_allowed : true
  const next = data?.next_change_at || hist?.next_change_at || ''
  const chosen = data && pick >= 0 ? data.titles[pick] : null
  const blocked = !!chosen && (chosen.over_limit || chosen.compliance.some(c => c.level === 'error'))

  return (
    <Drawer title="标题建议" width={560} onClose={onClose} foot={editable && data ? <>
      <input className="input" style={{ flex: 1 }} maxLength={200} value={reason} onChange={e => setReason(e.target.value)} placeholder="改标题的原因，可以不写" />
      <button className="btn btn-secondary" onClick={onClose}>取消</button>
      <button className="btn btn-primary" disabled={!chosen || blocked || !allowed || applying} onClick={apply}>{applying ? <Loader2 className="spin" /> : <Check />}用这个标题</button>
    </> : undefined}>
      <div className="stack" style={{ gap: 12 }}>
        {!allowed && <div className="ts-locked fs-12">{lockedText(next)}。可以先看建议，到时候再用。</div>}
        {editable && (
          <div className="ts-ask">
            <input className="input" maxLength={200} value={note} onChange={e => setNote(e.target.value)} onKeyDown={e => e.key === 'Enter' && !busy && run()} placeholder="补充要求，可以不写，如：突出 925 银扣" />
            <button className="btn btn-primary" disabled={busy} onClick={run}>{busy ? <Loader2 className="spin" /> : <Sparkles />}{busy ? '在想…' : data ? '再出 3 个' : '出 3 个标题'}</button>
          </div>
        )}
        {data && <>
          <div className="ts-current">
            <span className="t3 fs-12">现在</span>
            <span className="grow">{data.current.title || <span className="t4">（没有标题）</span>}</span>
            <span className="mono t3 fs-12">{data.current.chars}/{LIMIT}</span>
          </div>
          <div className="t4 fs-12">用了 {data.inputs.keywords} 个关键词、{data.inputs.competitors} 个竞品标题、{data.inputs.search_terms} 个成交搜索词、{data.inputs.history} 次改标题记录{data.inputs.keywords === 0 ? '。还没有关键词数据，建议先在「数据」页导入或采集' : ''}</div>
          <div className="ts-list">
            {data.titles.map((t, i) => {
              const errs = t.compliance.filter(c => c.level === 'error')
              const warns = t.compliance.filter(c => c.level === 'warn')
              const overlap = Math.round(t.overlap_bps / 100)
              return (
                <button key={i} className={'ts-card' + (pick === i ? ' active' : '')} disabled={!editable} onClick={() => setPick(i)}>
                  <div className="ts-head">
                    <span className="mono t4">{i + 1}</span>
                    {t.focus && <Pill kind="pill-neutral" dot={false}>{FOCUS[t.focus] || t.focus}</Pill>}
                    <span className="grow" />
                    <span className={'mono fs-12 ' + (t.over_limit ? 'ts-bad' : 't3')}>{t.chars}/{LIMIT}</span>
                  </div>
                  <div className="ts-title">{t.title}</div>
                  {t.keywords.length > 0 && <div className="ts-kw">{t.keywords.map(k => <span key={k.word} className="tag tag-blue" title={k.score == null ? '没有搜索量数据' : `价值 ${k.score.toFixed(2)}`}>{k.word}</span>)}</div>}
                  <div className="ts-meta fs-12">
                    <span className={overlap >= 50 ? 'ts-warn' : 't3'} title={t.overlap_with ? `最像：${t.overlap_with}` : ''}>和竞品重复 {overlap}%</span>
                    {errs.length > 0 && <Pill kind="pill-danger" dot={false}>必须改：{errs.map(c => c.word || c.msg).join('、')}</Pill>}
                    {warns.length > 0 && <Pill kind="pill-warning" dot={false}>{warns.map(c => c.msg || c.word).join('；')}</Pill>}
                    {errs.length === 0 && warns.length === 0 && <span className="ts-ok">合规</span>}
                  </div>
                  {t.note && <div className="t3 fs-12">{t.note}</div>}
                </button>
              )
            })}
          </div>
        </>}
        {!data && !busy && <Empty title="按数据写标题" desc="会带上本店这个品类近 30 天的高价值词、竞品标题和这款的成交搜索词，出 3 个标题；同一款 7 天内只能改一次。" />}
        {busy && !data && <div className="t3 fs-12 inline"><Loader2 className="spin" size={14} />AI 在写，一般 10–30 秒…</div>}

        <div className="ts-history">
          <div className="sub-title inline"><History size={14} />改标题记录</div>
          {!hist ? <span className="t4 fs-12">加载中…</span> : hist.items.length === 0 ? <span className="t4 fs-12">还没改过</span> : (
            <div className="list">{hist.items.map(h => (
              <div key={h.id} className="list-item">
                <div style={{ minWidth: 0, flex: 1 }}>
                  <div className="title">{h.new_title}</div>
                  <div className="meta"><span className="t4">原：</span>{h.old_title || '（空）'}</div>
                  <div className="meta">{fmtAgo(h.created_at)} · {h.created_by_name || '—'}{h.reason ? ` · ${h.reason}` : ''}</div>
                </div>
              </div>
            ))}</div>
          )}
        </div>
      </div>
    </Drawer>
  )
}
