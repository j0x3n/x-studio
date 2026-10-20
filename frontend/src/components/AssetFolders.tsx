import { useEffect, useRef, useState, type DragEvent, type ReactNode } from 'react'
import { ChevronRight, Folder, FolderOpen, FolderX, ImageOff, Search, Upload, X } from 'lucide-react'
import { api, qs, type Page } from '../lib/api'
import { useToast, errText } from '../lib/toast'
import type { AssetFolder, Product } from '../lib/types'
import { Drawer, Empty, Field, Loading } from './ui'

// 素材拖到文件夹上时用的数据类型
export const DRAG_ASSETS = 'application/x-xs-assets'
export const dragIds = (e: DragEvent) => {
  try { const v = JSON.parse(e.dataTransfer.getData(DRAG_ASSETS) || '[]'); return Array.isArray(v) ? v.map(Number).filter(Boolean) : [] } catch { return [] }
}
const hasAssets = (e: DragEvent) => e.dataTransfer.types.includes(DRAG_ASSETS)

export const childrenOf = (all: AssetFolder[], parent: number | null) => all.filter(f => (f.parent_id ?? null) === parent).sort((a, b) => a.name.localeCompare(b.name, 'zh-CN'))
// 从顶层到这个文件夹的路径
export function folderPath(all: AssetFolder[], id: number | null) {
  const out: AssetFolder[] = []
  const byId = new Map(all.map(f => [f.id, f]))
  let cur = id ? byId.get(id) : undefined
  while (cur && out.length < 10) { out.unshift(cur); cur = cur.parent_id ? byId.get(cur.parent_id) : undefined }
  return out
}
// 平铺成带层级的列表，菜单里「移到文件夹」用
export function flatFolders(all: AssetFolder[]) {
  const out: { f: AssetFolder; depth: number }[] = []
  const walk = (p: number | null, d: number) => childrenOf(all, p).forEach(f => { out.push({ f, depth: d }); walk(f.id, d + 1) })
  walk(null, 0)
  return out
}

type TreeProps = {
  folders: AssetFolder[]; current: string; unfiled?: number
  onOpen: (id: string) => void
  onMenu: (e: React.MouseEvent, f: AssetFolder | null) => void
  onDropAssets: (folderId: number | null, ids: number[]) => void
}

// 左侧文件夹树：点开看里面的图，右键管理，把图拖上来就移进去
export function FolderTree(p: TreeProps) {
  const [open, setOpen] = useState<Set<number>>(() => {
    try { return new Set(JSON.parse(localStorage.getItem('asset-folders-open') || '[]')) } catch { return new Set() }
  })
  // 当前文件夹的上级都自动展开
  useEffect(() => {
    const ids = folderPath(p.folders, Number(p.current) || null).slice(0, -1).map(f => f.id)
    if (ids.some(i => !open.has(i))) setOpen(o => new Set([...o, ...ids]))
  }, [p.current, p.folders]) // eslint-disable-line react-hooks/exhaustive-deps
  const toggle = (id: number) => setOpen(o => {
    const n = new Set(o)
    if (n.has(id)) n.delete(id); else n.add(id)
    try { localStorage.setItem('asset-folders-open', JSON.stringify([...n])) } catch { /* 无痕模式 */ }
    return n
  })
  const render = (parent: number | null, depth: number): ReactNode[] => childrenOf(p.folders, parent).flatMap(f => {
    const kids = p.folders.some(x => x.parent_id === f.id)
    const isOpen = open.has(f.id)
    return [
      <FolderRow key={f.id} depth={depth} active={p.current === String(f.id)} onClick={() => p.onOpen(String(f.id))}
        onMenu={e => p.onMenu(e, f)} onDrop={ids => p.onDropAssets(f.id, ids)}
        icon={p.current === String(f.id) ? <FolderOpen /> : <Folder />}
        caret={kids ? <button className={'caret' + (isOpen ? ' open' : '')} onClick={e => { e.stopPropagation(); toggle(f.id) }} aria-label={isOpen ? '收起' : '展开'}><ChevronRight /></button> : <span className="caret" />}
        count={f.count}>
        <span className="ellipsis">{f.name}</span>
      </FolderRow>,
      ...(kids && isOpen ? render(f.id, depth + 1) : [])
    ]
  })
  return (
    <div className="folder-tree" onContextMenu={e => p.onMenu(e, null)}>
      <FolderRow depth={0} active={p.current === '0'} onClick={() => p.onOpen('0')} onMenu={e => p.onMenu(e, null)}
        onDrop={ids => p.onDropAssets(null, ids)} icon={<FolderX />} caret={<span className="caret" />} count={p.unfiled}>
        <span className="ellipsis">未归类</span>
      </FolderRow>
      {render(null, 0)}
    </div>
  )
}

function FolderRow({ depth, active, onClick, onMenu, onDrop, icon, caret, count, children }: {
  depth: number; active: boolean; onClick: () => void; onMenu: (e: React.MouseEvent) => void; onDrop: (ids: number[]) => void
  icon: ReactNode; caret: ReactNode; count?: number; children: ReactNode
}) {
  const [over, setOver] = useState(false)
  return (
    <div className={'folder-node' + (active ? ' active' : '') + (over ? ' drop' : '')} style={{ ['--depth' as string]: depth }}
      onClick={onClick} onContextMenu={onMenu}
      onDragOver={e => { if (hasAssets(e)) { e.preventDefault(); e.dataTransfer.dropEffect = 'move'; setOver(true) } }}
      onDragLeave={() => setOver(false)}
      onDrop={e => { setOver(false); const ids = dragIds(e); if (ids.length) { e.preventDefault(); onDrop(ids) } }}>
      {caret}{icon}{children}{count !== undefined && count > 0 && <span className="count">{count}</span>}
    </div>
  )
}

// 子文件夹磁贴：在文件夹里时显示在网格上面
export function FolderTile({ f, onOpen, onMenu, onDrop }: { f: AssetFolder; onOpen: () => void; onMenu: (e: React.MouseEvent) => void; onDrop: (ids: number[]) => void }) {
  const [over, setOver] = useState(false)
  return (
    <div className={'lib-folder' + (over ? ' drop' : '')} onClick={onOpen} onContextMenu={onMenu} title={f.name}
      onDragOver={e => { if (hasAssets(e)) { e.preventDefault(); setOver(true) } }} onDragLeave={() => setOver(false)}
      onDrop={e => { setOver(false); const ids = dragIds(e); if (ids.length) { e.preventDefault(); onDrop(ids) } }}>
      <Folder /><span className="ellipsis">{f.name}</span>{f.count > 0 && <span className="count mono">{f.count}</span>}
    </div>
  )
}

// 起名字的小抽屉：新建文件夹、改素材显示名都用它
export function NameDialog({ title, label, initial = '', max = 30, hint, onClose, onSubmit }: {
  title: string; label: string; initial?: string; max?: number; hint?: string; onClose: () => void; onSubmit: (v: string) => Promise<void> | void
}) {
  const [v, setV] = useState(initial)
  const [busy, setBusy] = useState(false)
  const submit = async () => {
    if (!v.trim() || busy) return
    setBusy(true)
    try { await onSubmit(v.trim()) } finally { setBusy(false) }
  }
  return (
    <Drawer title={title} width={380} onClose={onClose} foot={<>
      <button className="btn btn-secondary" onClick={onClose}>取消</button>
      <button className="btn btn-primary" disabled={!v.trim() || busy} onClick={submit}>确定</button>
    </>}>
      <Field label={label} hint={hint}>
        <input className="input" autoFocus maxLength={max} value={v} onChange={e => setV(e.target.value)} onFocus={e => e.target.select()}
          onKeyDown={e => e.key === 'Enter' && submit()} />
      </Field>
    </Drawer>
  )
}

const LAST_PRODUCT = 'asset-upload-product'

// 上传：素材都挂在某一款商品下面（权限按商品算），所以要先选款；放进当前文件夹
export function UploadDialog({ files: initial, folder, folderName, productId, onClose, onDone }: {
  files: File[]; folder: number | null; folderName: string; productId?: number
  onClose: () => void; onDone: () => void
}) {
  const toast = useToast()
  const [files, setFiles] = useState<File[]>(initial)
  const [list, setList] = useState<Product[] | null>(null)
  const [q, setQ] = useState('')
  const [pid, setPid] = useState<number | null>(() => productId || Number(localStorage.getItem(LAST_PRODUCT)) || null)
  const [picked, setPicked] = useState<Product | null>(null)
  const [busy, setBusy] = useState(false)
  const [over, setOver] = useState(false)
  const input = useRef<HTMLInputElement>(null)
  useEffect(() => {
    const ctrl = new AbortController()
    const t = setTimeout(() => api<Page<Product>>('/api/products' + qs({ q, page_size: 30 }), { signal: ctrl.signal }).then(r => setList(r.items)).catch(() => !ctrl.signal.aborted && setList([])), 200)
    return () => { clearTimeout(t); ctrl.abort() }
  }, [q])
  useEffect(() => {
    if (pid && !picked) api<{ product: Product } | Product>(`/api/products/${pid}`).then(r => setPicked('product' in r ? r.product : r)).catch(() => setPid(null))
  }, [pid, picked])
  const add = (fl: FileList | File[]) => {
    const ok = Array.from(fl).filter(f => /^image\/(jpeg|png|webp)$/.test(f.type))
    if (ok.length < Array.from(fl).length) toast('只能上传 jpg / png / webp，其他文件已跳过', 'error')
    setFiles(old => [...old, ...ok])
  }
  const submit = async () => {
    if (!pid || !files.length) return
    setBusy(true)
    try {
      // 后端一次最多收 32 张，多了分批传
      for (let i = 0; i < files.length; i += 30) {
        const form = new FormData()
        files.slice(i, i + 30).forEach(f => form.append('files', f))
        await api(`/api/products/${pid}/assets` + qs({ folder_id: folder || undefined }), { form })
      }
      try { localStorage.setItem(LAST_PRODUCT, String(pid)) } catch { /* 无痕模式 */ }
      toast(`已上传 ${files.length} 张`, 'ok'); onDone()
    } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const size = files.reduce((a, f) => a + f.size, 0)
  return (
    <Drawer title="上传图片" width={480} onClose={onClose} foot={<>
      <span className="help grow">放到：{folderName}</span>
      <button className="btn btn-secondary" onClick={onClose}>取消</button>
      <button className="btn btn-primary" disabled={!pid || !files.length || busy} onClick={submit}><Upload />{busy ? '上传中…' : `上传 ${files.length} 张`}</button>
    </>}>
      <div className={'drop' + (over ? ' over' : '')} onClick={() => input.current?.click()}
        onDragOver={e => { e.preventDefault(); setOver(true) }} onDragLeave={() => setOver(false)}
        onDrop={e => { e.preventDefault(); setOver(false); add(e.dataTransfer.files) }}>
        <Upload /><b>点击或拖入图片</b>jpg / png / webp，单张不超过 25 MB
        <input ref={input} type="file" accept="image/jpeg,image/png,image/webp" multiple hidden onChange={e => { if (e.target.files) add(e.target.files); e.target.value = '' }} />
      </div>
      {files.length > 0 && <div className="upload-list">
        <div className="t3"><span className="grow">{files.length} 张 · {(size / 1048576).toFixed(1)} MB</span><button className="btn btn-ghost btn-sm" onClick={() => setFiles([])}>清空</button></div>
        {files.map((f, i) => <div key={i}><span className="grow ellipsis">{f.name}</span><span className="t4 mono">{(f.size / 1024).toFixed(0)} KB</span>
          <button className="tag-x" onClick={() => setFiles(l => l.filter((_, k) => k !== i))} aria-label="移除"><X /></button></div>)}
      </div>}
      <Field label="属于哪一款" req hint="素材按款管理权限，上传后也可以在素材库里「移到别的款」。">
        {picked && <div className="list-item" style={{ padding: '6px 8px' }}>
          {picked.cover_url ? <img className="cover" src={picked.cover_url} alt="" /> : <span className="cover"><ImageOff /></span>}
          <div className="grow" style={{ minWidth: 0 }}><div className="title ellipsis">{picked.name}</div><div className="meta mono">#{picked.id}</div></div>
          <button className="btn btn-ghost btn-sm" onClick={() => { setPicked(null); setPid(null) }}>换一款</button>
        </div>}
      </Field>
      {!picked && <>
        <label className="field"><Search /><input placeholder="搜款名" value={q} onChange={e => setQ(e.target.value)} /></label>
        {!list ? <Loading /> : list.length === 0 ? <Empty title="没有找到" /> : <div className="list">{list.map(p => (
          <label key={p.id} className="list-item" style={{ cursor: 'pointer' }}>
            <input type="radio" className="radio" checked={pid === p.id} onChange={() => { setPid(p.id); setPicked(p) }} />
            {p.cover_url ? <img className="cover" src={p.cover_url} alt="" /> : <span className="cover"><ImageOff /></span>}
            <div style={{ minWidth: 0 }}><div className="title ellipsis">{p.name}</div><div className="meta mono">#{p.id}</div></div>
          </label>
        ))}</div>}
      </>}
    </Drawer>
  )
}
