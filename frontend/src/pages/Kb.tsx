import { useCallback, useEffect, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { Plus, Search, Sparkles, Pin, FolderPlus, Pencil, Trash2, BookOpen } from 'lucide-react'
import { api, qs, type Page } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { fmtAgo } from '../lib/format'
import { plainText } from '../lib/markdown'
import { flattenFolders, type KbArticle, type KbFolder, type KbDraft } from '../lib/kb'
import { Topbar, Loading, Empty, Drawer, Field } from '../components/ui'

// 知识库首页：左边目录，右边文章列表；有 kb.edit 的可以新建、AI 整理、管目录
export function Kb() {
  const nav = useNavigate()
  const { can } = useAuth()
  const toast = useToast()
  const edit = can('kb.edit')
  const [sp, setSp] = useSearchParams()
  const folder = Number(sp.get('folder')) || 0
  const tag = sp.get('tag') || ''
  const [q, setQ] = useState('')
  const [folders, setFolders] = useState<KbFolder[] | null>(null)
  const [data, setData] = useState<Page<KbArticle> | null>(null)
  const [tags, setTags] = useState<{ tag: string; count: number }[]>([])
  const [organizing, setOrganizing] = useState(false)
  const [folderEdit, setFolderEdit] = useState<Partial<KbFolder> | null>(null)

  const loadFolders = useCallback(() => {
    api('/api/kb/folders').then(r => setFolders(flattenFolders(r))).catch(() => setFolders([]))
    api<{ tag: string; count: number }[] | { items: { tag: string; count: number }[] }>('/api/kb/tags').then(r => setTags(Array.isArray(r) ? r : r.items || [])).catch(() => setTags([]))
  }, [])
  useEffect(() => { loadFolders() }, [loadFolders])
  const load = useCallback(() => {
    api<Page<KbArticle>>('/api/kb/articles' + qs({ folder_id: folder || undefined, tag, q, page_size: 200 }))
      .then(setData).catch(e => { toast(errText(e), 'error'); setData({ items: [], total: 0 }) })
  }, [folder, tag, q, toast])
  useEffect(() => { const t = setTimeout(load, q ? 250 : 0); return () => clearTimeout(t) }, [load, q])

  const pick = (k: string, v: string | number) => {
    const n = new URLSearchParams(sp)
    if (v) n.set(k, String(v)); else n.delete(k)
    setSp(n, { replace: true })
  }
  const roots = (folders || []).filter(f => !f.parent_id).sort((a, b) => a.sort - b.sort || a.id - b.id)
  const children = (id: number) => (folders || []).filter(f => f.parent_id === id).sort((a, b) => a.sort - b.sort || a.id - b.id)
  const total = (folders || []).reduce((s, f) => s + (f.article_count || 0), 0)
  const cur = (folders || []).find(f => f.id === folder)
  const items = data?.items || []

  const delFolder = async (f: KbFolder) => {
    if (!confirm(`删除目录「${f.name}」？目录下有文章或子目录时不能删。`)) return
    try { await api(`/api/kb/folders/${f.id}`, { method: 'DELETE' }); if (folder === f.id) pick('folder', 0); loadFolders() } catch (e) { toast(errText(e), 'error') }
  }

  const folderRow = (f: KbFolder, depth: number) => (
    <div key={f.id}>
      <div className={'kb-folder' + (folder === f.id ? ' active' : '')} style={{ paddingLeft: 8 + depth * 14 }} onClick={() => pick('folder', f.id)}>
        <span className="ellipsis grow">{f.name}</span>
        {edit && <span className="kb-folder-ops">
          <button className="btn btn-ghost btn-icon btn-sm" title="改名" onClick={e => { e.stopPropagation(); setFolderEdit(f) }}><Pencil /></button>
          <button className="btn btn-ghost btn-icon btn-sm" title="删除" onClick={e => { e.stopPropagation(); delFolder(f) }}><Trash2 /></button>
        </span>}
        <span className="count mono">{f.article_count || ''}</span>
      </div>
      {depth === 0 && children(f.id).map(c => folderRow(c, 1))}
    </div>
  )

  return (
    <>
      <Topbar title="知识库" sub={folders ? `${total} 篇` : undefined}>
        {edit && <button className="btn btn-secondary" onClick={() => setOrganizing(true)}><Sparkles />AI 整理笔记</button>}
        {edit && <button className="btn btn-primary" onClick={() => nav('/kb/new' + (folder ? `?folder=${folder}` : ''))}><Plus />新建文章</button>}
      </Topbar>
      <div className="scroll"><main className="content">
        <div className="kb-layout">
          <aside className="panel kb-side">
            <div className={'kb-folder' + (!folder ? ' active' : '')} onClick={() => pick('folder', 0)}><BookOpen /><span className="grow">全部文章</span><span className="count mono">{total || ''}</span></div>
            <div className="kb-side-label"><span className="grow">目录</span>{edit && <button className="btn btn-ghost btn-icon btn-sm" title="新建目录" onClick={() => setFolderEdit({})}><FolderPlus /></button>}</div>
            {!folders ? <Loading /> : roots.map(f => folderRow(f, 0))}
            {tags.length > 0 && <>
              <div className="kb-side-label">标签</div>
              <div className="kb-tags">{tags.slice(0, 30).map(t => <button key={t.tag} className={'chip' + (tag === t.tag ? ' active' : '')} onClick={() => pick('tag', tag === t.tag ? '' : t.tag)}>{t.tag}<span className="t4">{t.count}</span></button>)}</div>
            </>}
          </aside>
          <section className="panel kb-main">
            <div className="toolbar">
              <span className="panel-title">{cur ? cur.name : '全部文章'}{tag && <span className="tag tag-blue" style={{ marginLeft: 8 }}>#{tag}</span>}</span>
              <span className="spacer" />
              <label className="field" style={{ width: 240 }}><Search /><input placeholder="搜索标题和正文，空格分词" value={q} onChange={e => setQ(e.target.value)} /></label>
            </div>
            {!data ? <Loading /> : items.length === 0 ? (
              <Empty title={q || tag ? '没有找到文章' : '这里还没有文章'} desc={q || tag ? '换个词试试。' : edit ? '点右上角「新建文章」，或把零散笔记交给「AI 整理笔记」。' : undefined} />
            ) : (
              <div className="kb-list">
                {items.map(a => (
                  <div key={a.id} className="kb-row" onClick={() => nav(`/kb/${a.id}`)}>
                    <div className="kb-row-title">{a.pinned && <Pin className="kb-pin" />}<span className="ellipsis">{a.title}</span></div>
                    {a.summary && <div className="kb-row-sum">{plainText(a.summary)}</div>}
                    <div className="kb-row-meta">
                      {!folder && <span>{a.folder?.name}</span>}
                      {a.tags.slice(0, 4).map(t => <span key={t} className="tag tag-gray">{t}</span>)}
                      {a.links.slice(0, 2).map(l => <span key={l.type + l.id} className="tag tag-blue">{l.label}</span>)}
                      <span className="grow" />
                      <span>{a.updated_by?.name} · {fmtAgo(a.updated_at)}</span>
                    </div>
                  </div>
                ))}
              </div>
            )}
            {data && data.total > items.length && <div className="pager">只显示前 {items.length} 篇，共 {data.total} 篇</div>}
          </section>
        </div>
      </main></div>
      {organizing && <OrganizeDrawer folders={folders || []} folder={folder} onClose={() => setOrganizing(false)} onDone={d => nav('/kb/new', { state: { draft: d } })} />}
      {folderEdit && <FolderDrawer f={folderEdit} roots={roots} onClose={() => setFolderEdit(null)} onDone={() => { setFolderEdit(null); loadFolders() }} />}
    </>
  )
}

function FolderDrawer({ f, roots, onClose, onDone }: { f: Partial<KbFolder>; roots: KbFolder[]; onClose: () => void; onDone: () => void }) {
  const toast = useToast()
  const [name, setName] = useState(f.name || '')
  const [parent, setParent] = useState(f.parent_id ? String(f.parent_id) : '')
  const [sort, setSort] = useState(String(f.sort ?? 0))
  const [busy, setBusy] = useState(false)
  const hasKids = !!f.id && roots.some(r => r.id === f.id)
  const save = async () => {
    setBusy(true)
    try {
      const body = { name: name.trim(), parent_id: parent ? Number(parent) : null, sort: Number(sort) || 0 }
      if (f.id) await api(`/api/kb/folders/${f.id}`, { method: 'PATCH', body }); else await api('/api/kb/folders', { body })
      onDone()
    } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  return (
    <Drawer title={f.id ? '修改目录' : '新建目录'} width={380} onClose={onClose}
      foot={<><span className="grow" /><button className="btn btn-secondary" onClick={onClose}>取消</button><button className="btn btn-primary" disabled={busy || !name.trim()} onClick={save}>保存</button></>}>
      <Field label="名称" req><input className="input" autoFocus maxLength={30} value={name} onChange={e => setName(e.target.value)} /></Field>
      <Field label="上级目录" hint="最多两层">
        <select className="input" value={parent} onChange={e => setParent(e.target.value)} disabled={hasKids && !f.parent_id}>
          <option value="">无（一级目录）</option>
          {roots.filter(r => r.id !== f.id).map(r => <option key={r.id} value={r.id}>{r.name}</option>)}
        </select>
      </Field>
      <Field label="排序" hint="数字小的在前"><input className="input mono" type="number" value={sort} onChange={e => setSort(e.target.value)} /></Field>
    </Drawer>
  )
}

function OrganizeDrawer({ folders, folder, onClose, onDone }: { folders: KbFolder[]; folder: number; onClose: () => void; onDone: (d: KbDraft) => void }) {
  const toast = useToast()
  const [text, setText] = useState('')
  const [fid, setFid] = useState(String(folder || folders[0]?.id || ''))
  const [busy, setBusy] = useState(false)
  const run = async () => {
    setBusy(true)
    try {
      const d = await api<KbDraft>('/api/kb/organize', { body: { text, folder_id: Number(fid) || null } })
      onDone({ ...d, tags: d.tags || [] })
    } catch (e) { toast(errText(e), 'error'); setBusy(false) }
  }
  return (
    <Drawer title="AI 整理笔记" width={560} onClose={onClose}
      foot={<><span className="t3 fs-12">整理结果会打开在编辑器里，确认后再保存</span><span className="grow" /><button className="btn btn-secondary" onClick={onClose}>取消</button><button className="btn btn-primary" disabled={busy || !text.trim()} onClick={run}><Sparkles />{busy ? '整理中…' : '开始整理'}</button></>}>
      <Field label="零散的笔记或聊天记录" hint={`${text.length} / 20000 字。AI 只整理不添加，材料里没有的不会写。`}>
        <textarea className="textarea" style={{ minHeight: 320 }} maxLength={20000} autoFocus value={text} onChange={e => setText(e.target.value)} placeholder="把微信聊天、备忘录、语音转的文字直接贴进来" />
      </Field>
      <Field label="放进哪个目录" hint="AI 也会自己判断，选不出来时用这里选的">
        <select className="input" value={fid} onChange={e => setFid(e.target.value)}>
          {folders.map(f => <option key={f.id} value={f.id}>{f.parent_id ? '　' : ''}{f.name}</option>)}
        </select>
      </Field>
    </Drawer>
  )
}
