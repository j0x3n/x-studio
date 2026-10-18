import { useCallback, useEffect, useMemo, useRef, useState, type ClipboardEvent } from 'react'
import { Link, useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { Heading2, Heading3, Bold, List, ListOrdered, Quote, Table2, Link2, ImagePlus, X, Plus, Search, Save } from 'lucide-react'
import { api, qs, ApiError, type Page } from '../lib/api'
import { useToast, errText } from '../lib/toast'
import { useCategories } from '../lib/categories'
import { renderMarkdown } from '../lib/markdown'
import { normDetail, flattenFolders, linkTypeName, type KbFolder, type KbLink, type KbLinkType, type KbDraft } from '../lib/kb'
import { Topbar, Loading, Field, Drawer } from '../components/ui'

type Mode = 'split' | 'edit' | 'preview'
const MODE_KEY = 'kb.editor.mode'

// 新建和编辑文章：左边 Markdown，右边实时预览；图片、视频上传后插到光标处，也可以直接粘贴截图
export function KbEdit() {
  const { id } = useParams()
  const isNew = !id
  const nav = useNavigate()
  const loc = useLocation()
  const [sp] = useSearchParams()
  const toast = useToast()
  const draft = (loc.state as { draft?: KbDraft } | null)?.draft
  const [folders, setFolders] = useState<KbFolder[]>([])
  const [ready, setReady] = useState(isNew)
  const [title, setTitle] = useState(draft?.title || '')
  const [folderId, setFolderId] = useState<number>(draft?.folder_id || Number(sp.get('folder')) || 0)
  const [summary, setSummary] = useState(draft?.summary || '')
  const [tags, setTags] = useState<string[]>(draft?.tags || [])
  const [body, setBody] = useState(draft?.body || '')
  const [links, setLinks] = useState<KbLink[]>([])
  const [base, setBase] = useState(0)
  const [note, setNote] = useState(draft ? 'AI 整理' : '')
  const [dirty, setDirty] = useState(!!draft)
  const [busy, setBusy] = useState(false)
  const [uploading, setUploading] = useState(0)
  const [conflict, setConflict] = useState('')
  const [linking, setLinking] = useState(false)
  const [mode, setMode] = useState<Mode>(() => { try { return (localStorage.getItem(MODE_KEY) as Mode) || 'split' } catch { return 'split' } })
  const ta = useRef<HTMLTextAreaElement>(null)
  const fileIn = useRef<HTMLInputElement>(null)

  useEffect(() => {
    api('/api/kb/folders').then(r => {
      const list = flattenFolders(r)
      setFolders(list)
      setFolderId(f => f || list[0]?.id || 0)
    }).catch(() => setFolders([]))
  }, [])
  useEffect(() => {
    if (isNew) return
    api(`/api/kb/articles/${id}`).then(r => {
      const a = normDetail(r)
      setTitle(a.title); setFolderId(a.folder.id); setSummary(a.summary || ''); setTags(a.tags); setBody(a.body); setLinks(a.links); setBase(a.version)
      setReady(true)
    }).catch(e => { toast(errText(e), 'error'); nav('/kb') })
  }, [id, isNew, nav, toast])
  // 有没保存的改动时，关页面前提醒
  useEffect(() => {
    if (!dirty) return
    const f = (e: BeforeUnloadEvent) => { e.preventDefault(); e.returnValue = '' }
    addEventListener('beforeunload', f)
    return () => removeEventListener('beforeunload', f)
  }, [dirty])

  const touch = <T,>(set: (v: T) => void) => (v: T) => { set(v); setDirty(true) }
  const pickMode = (m: Mode) => { setMode(m); try { localStorage.setItem(MODE_KEY, m) } catch { /* 无痕模式 */ } }
  const html = useMemo(() => renderMarkdown(body), [body])

  // 在光标处插入：wrap 时包住选中的文字，line 时插在行首
  const insert = useCallback((before: string, after = '', line = false) => {
    const el = ta.current
    if (!el) { setBody(b => b + before + after); return }
    const s = el.selectionStart, e = el.selectionEnd
    const v = el.value
    let start = s
    if (line) start = v.lastIndexOf('\n', s - 1) + 1
    const sel = v.slice(s, e)
    const next = line ? v.slice(0, start) + before + v.slice(start) : v.slice(0, s) + before + sel + after + v.slice(e)
    setBody(next); setDirty(true)
    requestAnimationFrame(() => {
      el.focus()
      const pos = line ? s + before.length : s + before.length + sel.length
      el.setSelectionRange(pos, pos)
    })
  }, [])

  const upload = async (files: File[]) => {
    for (const f of files) {
      setUploading(n => n + 1)
      try {
        const form = new FormData()
        form.append('file', f)
        if (!isNew) form.append('article_id', String(id))
        const r = await api<{ url: string; name: string; mime: string }>('/api/kb/files', { form })
        const name = (r.name || f.name).replace(/[[\]]/g, '')
        insert(`\n![${name}](${r.url})\n`)
      } catch (e) { toast(`${f.name}：${errText(e)}`, 'error') } finally { setUploading(n => n - 1) }
    }
  }
  const onPaste = (e: ClipboardEvent<HTMLTextAreaElement>) => {
    const files = Array.from(e.clipboardData.files || []).filter(f => /^image\//.test(f.type))
    if (files.length) { e.preventDefault(); upload(files) }
  }

  const save = async () => {
    if (!title.trim()) { toast('写个标题', 'error'); return }
    if (!folderId) { toast('选一个目录', 'error'); return }
    setBusy(true); setConflict('')
    const payload = { title: title.trim(), body, summary: summary.trim(), tags, folder_id: folderId, links: links.map(l => ({ type: l.type, id: l.id })), note: note.trim() }
    try {
      if (isNew) {
        const r = await api<{ id?: number; article?: { id: number } }>('/api/kb/articles', { body: payload })
        setDirty(false)
        nav(`/kb/${r.article?.id ?? r.id}`, { replace: true })
      } else {
        await api(`/api/kb/articles/${id}`, { method: 'PATCH', body: { ...payload, base_version: base } })
        setDirty(false)
        nav(`/kb/${id}`)
      }
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) setConflict(e.message)
      else toast(errText(e), 'error')
    } finally { setBusy(false) }
  }

  if (!ready) return <><Topbar crumbs={['知识库', '…']} /><Loading /></>
  const roots = folders.filter(f => !f.parent_id)
  return (
    <>
      <Topbar crumbs={[<Link key="k" to="/kb">知识库</Link>]} title={isNew ? '新建文章' : '编辑文章'} sub={!isNew ? `基于第 ${base} 版` : draft ? 'AI 整理的草稿，确认后保存' : undefined}>
        <input className="input" style={{ width: 220 }} placeholder="这次改了什么（可不填）" maxLength={100} value={note} onChange={e => setNote(e.target.value)} />
        <button className="btn btn-secondary" onClick={() => { if (!dirty || confirm('放弃没保存的改动？')) nav(isNew ? '/kb' : `/kb/${id}`) }}>取消</button>
        <button className="btn btn-primary" disabled={busy || uploading > 0} onClick={save}><Save />{busy ? '保存中…' : '保存'}</button>
      </Topbar>
      <div className="scroll"><main className="content kb-edit">
        {conflict && <div className="kb-conflict">{conflict}。你的内容还在下面，先 <a className="link" href={`/kb/${id}`} target="_blank" rel="noreferrer">在新标签页打开最新版本</a> 对照合并，再刷新本页重新编辑。</div>}
        <section className="panel panel-section kb-form">
          <input className="input kb-title-input" placeholder="标题，说清楚讲的是什么" maxLength={100} value={title} onChange={e => touch(setTitle)(e.target.value)} />
          <div className="kb-form-row">
            <Field label="目录" req>
              <select className="input" value={folderId} onChange={e => touch(setFolderId)(Number(e.target.value))}>
                {roots.map(r => [<option key={r.id} value={r.id}>{r.name}</option>, ...folders.filter(c => c.parent_id === r.id).map(c => <option key={c.id} value={c.id}>　{c.name}</option>)])}
              </select>
            </Field>
            <Field label="标签" hint="回车添加，最多 10 个"><TagInput value={tags} onChange={touch(setTags)} /></Field>
            <Field label="关联">
              <div className="kb-link-chips">
                {links.map(l => <span key={l.type + l.id} className="chip outline">{linkTypeName[l.type]}：{l.label}<button className="tag-x" onClick={() => touch(setLinks)(links.filter(x => !(x.type === l.type && x.id === l.id)))}><X /></button></span>)}
                <button className="btn btn-ghost btn-sm" onClick={() => setLinking(true)}><Plus />关联品类 / 商品 / 物料</button>
              </div>
            </Field>
          </div>
          <Field label="摘要" hint="一句话，列表里显示；不填就取正文开头"><input className="input" maxLength={200} value={summary} onChange={e => touch(setSummary)(e.target.value)} /></Field>
        </section>
        <section className={'panel kb-editor ' + mode}>
          <div className="kb-editor-bar">
            <button className="btn btn-ghost btn-icon btn-sm" title="二级标题" onClick={() => insert('## ', '', true)}><Heading2 /></button>
            <button className="btn btn-ghost btn-icon btn-sm" title="三级标题" onClick={() => insert('### ', '', true)}><Heading3 /></button>
            <button className="btn btn-ghost btn-icon btn-sm" title="粗体" onClick={() => insert('**', '**')}><Bold /></button>
            <span className="vr" />
            <button className="btn btn-ghost btn-icon btn-sm" title="列表" onClick={() => insert('- ', '', true)}><List /></button>
            <button className="btn btn-ghost btn-icon btn-sm" title="步骤" onClick={() => insert('1. ', '', true)}><ListOrdered /></button>
            <button className="btn btn-ghost btn-icon btn-sm" title="引用" onClick={() => insert('> ', '', true)}><Quote /></button>
            <button className="btn btn-ghost btn-icon btn-sm" title="表格" onClick={() => insert('\n| 项目 | 说明 |\n| --- | --- |\n|  |  |\n')}><Table2 /></button>
            <button className="btn btn-ghost btn-icon btn-sm" title="链接" onClick={() => insert('[', '](https://)')}><Link2 /></button>
            <button className="btn btn-ghost btn-sm" title="上传图片或视频" onClick={() => fileIn.current?.click()}><ImagePlus />{uploading ? `上传中 ${uploading}` : '图片 / 视频'}</button>
            <input ref={fileIn} type="file" hidden multiple accept="image/jpeg,image/png,image/webp,image/gif,video/mp4,video/webm" onChange={e => { upload(Array.from(e.target.files || [])); e.target.value = '' }} />
            <span className="spacer" />
            <div className="segmented">
              {([['edit', '编辑'], ['split', '并排'], ['preview', '预览']] as [Mode, string][]).map(([k, l]) => <button key={k} className={mode === k ? 'active' : ''} onClick={() => pickMode(k)}>{l}</button>)}
            </div>
          </div>
          <div className="kb-editor-body">
            {mode !== 'preview' && <textarea ref={ta} className="kb-src mono" spellCheck={false} value={body} onChange={e => touch(setBody)(e.target.value)} onPaste={onPaste}
              placeholder={'用 Markdown 写：\n## 小节标题\n1. 第一步\n- 要点\n| 表头 | 表头 |\n截图可以直接粘贴进来'} />}
            {mode !== 'edit' && <div className="kb-preview md" dangerouslySetInnerHTML={{ __html: html || '<p class="t4">预览会显示在这里</p>' }} />}
          </div>
        </section>
      </main></div>
      {linking && <LinkPicker have={links} onClose={() => setLinking(false)} onPick={l => { touch(setLinks)([...links, l]) }} />}
    </>
  )
}

function TagInput({ value, onChange }: { value: string[]; onChange: (v: string[]) => void }) {
  const [t, setT] = useState('')
  const add = () => {
    const s = t.trim().slice(0, 20)
    if (s && !value.includes(s) && value.length < 10) onChange([...value, s])
    setT('')
  }
  return (
    <div className="kb-tag-input input">
      {value.map(v => <span key={v} className="chip neutral">{v}<button className="tag-x" onClick={() => onChange(value.filter(x => x !== v))}><X /></button></span>)}
      <input value={t} placeholder={value.length ? '' : '如 珍珠、鉴别'} onChange={e => setT(e.target.value)}
        onKeyDown={e => { if (e.key === 'Enter' || e.key === ',' || e.key === '，') { e.preventDefault(); add() } else if (e.key === 'Backspace' && !t && value.length) onChange(value.slice(0, -1)) }} onBlur={add} />
    </div>
  )
}

type Cand = { id: number; label: string }
function LinkPicker({ have, onClose, onPick }: { have: KbLink[]; onClose: () => void; onPick: (l: KbLink) => void }) {
  const [type, setType] = useState<KbLinkType>('product')
  const [q, setQ] = useState('')
  const [list, setList] = useState<Cand[] | null>(null)
  const [err, setErr] = useState('')
  const { items: cats } = useCategories(true)
  useEffect(() => {
    setErr('')
    if (type === 'category') { setList((cats || []).filter(c => !q || c.name.includes(q)).map(c => ({ id: c.id, label: c.name }))); return }
    const t = setTimeout(() => {
      const req = type === 'product'
        ? api<Page<{ id: number; name: string }>>('/api/products' + qs({ q, page_size: 30 })).then(r => r.items.map(p => ({ id: p.id, label: p.name })))
        : api<Page<{ id: number; code: string; name: string }>>('/api/inventory/items' + qs({ q, page_size: 30 })).then(r => r.items.map(i => ({ id: i.id, label: `${i.code} ${i.name}` })))
      req.then(setList).catch(e => { setList([]); setErr(e instanceof ApiError && e.status === 404 ? '库存模块还没上线' : errText(e)) })
    }, 200)
    return () => clearTimeout(t)
  }, [type, q, cats])
  const taken = (c: Cand) => have.some(l => l.type === type && l.id === c.id)
  return (
    <Drawer title="关联" width={420} onClose={onClose}>
      <div className="segmented">{(['product', 'category', 'item'] as KbLinkType[]).map(k => <button key={k} className={type === k ? 'active' : ''} onClick={() => { setType(k); setList(null) }}>{linkTypeName[k]}</button>)}</div>
      <label className="field"><Search /><input autoFocus placeholder={type === 'item' ? '编码或名称' : '名称'} value={q} onChange={e => setQ(e.target.value)} /></label>
      {err ? <div className="t3">{err}</div> : !list ? <Loading /> : list.length === 0 ? <div className="t4">没有找到</div> : (
        <div className="list">{list.map(c => (
          <div key={c.id} className="list-item">
            <span className="title ellipsis grow">{c.label}</span>
            {taken(c) ? <span className="t4 fs-12">已关联</span> : <button className="btn btn-secondary btn-sm" onClick={() => onPick({ type, id: c.id, label: c.label, path: '' })}>关联</button>}
          </div>
        ))}</div>
      )}
    </Drawer>
  )
}
