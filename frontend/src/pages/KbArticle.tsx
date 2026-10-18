import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { Pencil, History, Trash2, Pin, RotateCcw, Paperclip } from 'lucide-react'
import { api } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { fmtAgo, fmtDate } from '../lib/format'
import { renderMarkdown } from '../lib/markdown'
import { normDetail, diffLines, linkTypeName, fmtSize, type KbDetail, type KbVersion } from '../lib/kb'
import { Topbar, Loading, Drawer, Empty } from '../components/ui'

export function KbArticle() {
  const { id } = useParams()
  const nav = useNavigate()
  const { can } = useAuth()
  const toast = useToast()
  const edit = can('kb.edit')
  const [a, setA] = useState<KbDetail | null>(null)
  const [history, setHistory] = useState(false)
  const load = useCallback(() => {
    api(`/api/kb/articles/${id}`).then(r => setA(normDetail(r))).catch(e => { toast(errText(e), 'error'); nav('/kb') })
  }, [id, nav, toast])
  useEffect(() => { load() }, [load])
  const html = useMemo(() => renderMarkdown(a?.body || ''), [a?.body])
  if (!a) return <><Topbar crumbs={['知识库', '…']} /><Loading /></>

  const del = async () => {
    if (!confirm(`删除「${a.title}」？删除后管理员可以在回收站恢复。`)) return
    try { await api(`/api/kb/articles/${a.id}`, { method: 'DELETE' }); toast('已删除', 'ok'); nav('/kb') } catch (e) { toast(errText(e), 'error') }
  }
  const togglePin = async () => {
    try { await api(`/api/kb/articles/${a.id}`, { method: 'PATCH', body: { pinned: !a.pinned, base_version: a.version } }); load() } catch (e) { toast(errText(e), 'error') }
  }

  return (
    <>
      <Topbar crumbs={[<Link key="k" to="/kb">知识库</Link>, <Link key="f" to={`/kb?folder=${a.folder.id}`}>{a.folder.name}</Link>]} title={a.title} sub={`第 ${a.version} 版 · ${a.updated_by?.name || ''} ${fmtAgo(a.updated_at)}更新`}>
        <button className="btn btn-secondary" onClick={() => setHistory(true)}><History />历史版本</button>
        {edit && <button className="btn btn-ghost btn-icon" title={a.pinned ? '取消置顶' : '置顶'} onClick={togglePin}><Pin className={a.pinned ? 'kb-pin' : ''} /></button>}
        {edit && <button className="btn btn-ghost btn-icon" title="删除" onClick={del}><Trash2 /></button>}
        {edit && <button className="btn btn-primary" onClick={() => nav(`/kb/${a.id}/edit`)}><Pencil />编辑</button>}
      </Topbar>
      <div className="scroll"><main className="content">
        <div className="kb-read">
          <article className="panel kb-article">
            {a.summary && <p className="kb-summary">{a.summary}</p>}
            {a.body.trim() ? <div className="md" dangerouslySetInnerHTML={{ __html: html }} /> : <Empty title="正文是空的" />}
          </article>
          <aside className="kb-aside">
            <section className="panel panel-section">
              <div className="section-title">关联</div>
              {a.links.length === 0 ? <div className="t4 fs-12">没有关联品类、商品或物料</div> : (
                <div className="kb-links">{a.links.map(l => (
                  <div key={l.type + l.id} className="kb-link"><span className="t3">{linkTypeName[l.type]}</span>{l.path ? <Link className="link ellipsis" to={l.path}>{l.label}</Link> : <span className="t4">{l.label}</span>}</div>
                ))}</div>
              )}
            </section>
            {a.tags.length > 0 && (
              <section className="panel panel-section">
                <div className="section-title">标签</div>
                <div className="kb-tags">{a.tags.map(t => <Link key={t} className="chip outline" to={`/kb?tag=${encodeURIComponent(t)}`}>{t}</Link>)}</div>
              </section>
            )}
            {a.files.length > 0 && (
              <section className="panel panel-section">
                <div className="section-title">附件</div>
                <div className="kb-links">{a.files.map(f => (
                  <a key={f.id} className="kb-link link" href={f.url} target="_blank" rel="noreferrer"><Paperclip /><span className="ellipsis">{f.name}</span><span className="t4 fs-12">{fmtSize(f.size)}</span></a>
                ))}</div>
              </section>
            )}
            <section className="panel panel-section kb-meta">
              <div><span className="t3">创建</span>{fmtDate(a.created_at, true)}</div>
              <div><span className="t3">更新</span>{a.updated_by?.name} · {fmtDate(a.updated_at, true)}</div>
            </section>
          </aside>
        </div>
      </main></div>
      {history && <HistoryDrawer a={a} edit={edit} onClose={() => setHistory(false)} onReverted={() => { setHistory(false); load() }} />}
    </>
  )
}

// 历史版本：选一个版本看它和当前版本（或另一个版本）的差异，可以回退
function HistoryDrawer({ a, edit, onClose, onReverted }: { a: KbDetail; edit: boolean; onClose: () => void; onReverted: () => void }) {
  const toast = useToast()
  const versions = [...a.versions].sort((x, y) => y.version - x.version)
  const [left, setLeft] = useState<number>(versions[1]?.version || versions[0]?.version || a.version)
  const [right, setRight] = useState<number>(a.version)
  const [cache, setCache] = useState<Record<number, KbVersion>>({})
  useEffect(() => {
    for (const v of [left, right]) {
      if (cache[v]) continue
      api<KbVersion | { version: KbVersion }>(`/api/kb/articles/${a.id}/versions/${v}`)
        .then(r => setCache(c => ({ ...c, [v]: 'title' in r ? r : r.version })))
        .catch(e => toast(errText(e), 'error'))
    }
  }, [left, right]) // eslint-disable-line react-hooks/exhaustive-deps
  const L = cache[left], R = cache[right]
  const lines = useMemo(() => (L && R ? diffLines(L.body, R.body) : []), [L, R])
  const adds = lines.filter(l => l.kind === 'add').length, dels = lines.filter(l => l.kind === 'del').length
  const revert = async (v: number) => {
    if (!confirm(`用第 ${v} 版的内容覆盖当前内容？会生成新的一版，历史不会丢。`)) return
    try { await api(`/api/kb/articles/${a.id}/revert`, { body: { version: v, base_version: a.version } }); toast(`已回退到第 ${v} 版`, 'ok'); onReverted() } catch (e) { toast(errText(e), 'error') }
  }
  const pick = (v: number) => { if (v === right) return; setLeft(v) }
  return (
    <Drawer title="历史版本" width={980} onClose={onClose}>
      <div className="kb-history">
        <div className="kb-vlist">
          {versions.map(v => (
            <div key={v.version} className={'kb-ver' + (v.version === left ? ' left' : '') + (v.version === right ? ' right' : '')} onClick={() => pick(v.version)}>
              <div className="between"><span className="mono">第 {v.version} 版</span>{v.version === a.version ? <span className="tag tag-green">当前</span> : edit && <button className="btn btn-ghost btn-sm" title="回退到这一版" onClick={e => { e.stopPropagation(); revert(v.version) }}><RotateCcw />回退</button>}</div>
              <div className="t3 fs-12">{v.user?.name} · {fmtDate(v.created_at, true)}</div>
              {v.note && <div className="fs-12 ellipsis">{v.note}</div>}
            </div>
          ))}
        </div>
        <div className="kb-diff">
          <div className="toolbar">
            <span>对比</span>
            <select className="input" style={{ width: 110 }} value={left} onChange={e => setLeft(Number(e.target.value))}>{versions.map(v => <option key={v.version} value={v.version}>第 {v.version} 版</option>)}</select>
            <span className="t3">→</span>
            <select className="input" style={{ width: 110 }} value={right} onChange={e => setRight(Number(e.target.value))}>{versions.map(v => <option key={v.version} value={v.version}>第 {v.version} 版</option>)}</select>
            <span className="spacer" />
            {L && R && <span className="fs-12"><span className="kb-add-n">+{adds}</span> <span className="kb-del-n">−{dels}</span></span>}
          </div>
          {!L || !R ? <Loading /> : (
            <>
              {L.title !== R.title && <div className="kb-dline del">标题：{L.title}</div>}
              {L.title !== R.title && <div className="kb-dline add">标题：{R.title}</div>}
              {left === right || (adds === 0 && dels === 0 && L.title === R.title) ? <Empty title="两个版本的正文一样" /> : (
                <div className="kb-dbody">{lines.map((l, i) => <div key={i} className={'kb-dline ' + l.kind}>{l.text || ' '}</div>)}</div>
              )}
            </>
          )}
        </div>
      </div>
    </Drawer>
  )
}
