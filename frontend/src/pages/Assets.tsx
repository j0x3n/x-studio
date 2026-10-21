import { useCallback, useEffect, useMemo, useRef, useState, type DragEvent, type MouseEvent as RMouseEvent } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import {
  Search, Images, Archive, ArchiveRestore, Layers, X, Wand2, Tags, Download, FolderInput, Trash2, ArrowLeft, ImageOff, Shirt,
  Upload, FolderPlus, Folder, FolderOpen, Maximize2, PanelRight, Pencil, Sparkles, SlidersHorizontal, ScanSearch, Package, RefreshCw, CheckSquare, FolderX, Loader2, Film, Play
} from 'lucide-react'
import { api, downloadPost, qs, type Page } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { useJobs, jobRunning } from '../lib/jobs'
import type { Asset, AssetFolder, Job, ProcessOps, Product } from '../lib/types'
import { Topbar, Pill, Loading, Empty, Drawer, Field } from '../components/ui'
import { AssetInspector } from '../components/AssetInspector'
import { useLightbox } from '../components/Lightbox'
import { useContextMenu, type MenuEntry } from '../components/ContextMenu'
import { FolderTree, FolderTile, NameDialog, UploadDialog, DRAG_ASSETS, childrenOf, flatFolders, folderPath } from '../components/AssetFolders'
import { sourceName, assetTitle, quickOps, fmtDuration } from '../lib/format'
import { useCategories, useCodeName } from '../lib/categories'
import { LibrarySwitch } from '../components/LibrarySwitch'

const PAGE = 60
type Hit = Asset & { distance?: number }
type Item = MenuEntry | false | null | undefined
// 去掉没权限的项和多余的分隔线
const menuOf = (list: Item[]) => list.filter((x): x is MenuEntry => !!x).filter((x, i, a) => x !== 'sep' || (i > 0 && i < a.length - 1 && a[i - 1] !== 'sep'))
type NameDlg = { mode: 'folder'; parent: number | null; moveIds?: number[] } | { mode: 'rename-folder'; folder: AssetFolder } | { mode: 'rename-asset'; asset: Asset }

// 素材库：左边文件夹和筛选，中间网格，点开一张在右边看详情；勾选后底部出批量操作，右键有全部操作
export function Assets() {
  const { me, can } = useAuth()
  const toast = useToast()
  const nav = useNavigate()
  const lightbox = useLightbox()
  const menu = useContextMenu()
  const [sp, setSp] = useSearchParams()
  const codeName = useCodeName()
  const { items: cats } = useCategories()
  // 照片类型下拉：所有品类的编号按组合并，同一编号只列一次
  const codeGroups = useMemo(() => {
    const groups = new Map<string, { group: string; prefix: string; codes: { code: string; name: string }[] }>()
    for (const c of (cats || []).flatMap(x => x.codes || [])) {
      const g = groups.get(c.group) || { group: c.group.replace(/^[A-Z]\s*/, ''), prefix: c.code.slice(0, 1), codes: [] }
      if (!g.codes.some(x => x.code === c.code)) g.codes.push({ code: c.code, name: c.name })
      groups.set(c.group, g)
    }
    return [...groups.values()]
  }, [cats])
  const f = {
    q: sp.get('q') || '', source: sp.get('source') || '', kind: sp.get('kind') || '', tag: sp.get('tag') || '',
    code: sp.get('code') || '', shop_id: sp.get('shop_id') || '', product_id: sp.get('product_id') || '',
    from: sp.get('from') || '', to: sp.get('to') || '', sort: sp.get('sort') || 'new', folder: sp.get('folder') || '',
    root: sp.get('root') === '1', archived: sp.get('archived') === '1', page: Number(sp.get('page') || 1),
    media: sp.get('media') || '', stale: sp.get('stale') || ''
  }
  const set = (patch: Record<string, string | number | boolean>) => {
    const next = new URLSearchParams(sp)
    for (const [k, v] of Object.entries(patch)) {
      if (v === '' || v === false || (k === 'sort' && v === 'new')) next.delete(k)
      else next.set(k, v === true ? '1' : String(v))
    }
    if (!('page' in patch)) next.delete('page')
    setSp(next, { replace: true })
  }

  const [data, setData] = useState<Page<Hit> | null>(null)
  const [folders, setFolders] = useState<AssetFolder[]>([])
  const [q, setQ] = useState(f.q)
  const [selected, setSelected] = useState<Set<number>>(new Set())
  const [active, setActive] = useState<number | null>(null)
  const [similar, setSimilar] = useState<Asset | null>(null)
  const [dialog, setDialog] = useState<'tag_add' | 'tag_remove' | 'move' | null>(null)
  const [nameDlg, setNameDlg] = useState<NameDlg | null>(null)
  const [upload, setUpload] = useState<File[] | null>(null)
  const [fileOver, setFileOver] = useState(false)
  const [dragging, setDragging] = useState<number[]>([])
  const [jobIds, setJobIds] = useState<number[]>([])
  const jobs = useJobs(jobIds)
  const last = useRef<number | null>(null)
  const key = sp.toString()

  const load = useCallback(() => {
    if (similar) {
      api<Page<Hit>>(`/api/assets/${similar.id}/similar`, { method: 'POST' }).then(setData).catch(e => { toast(errText(e), 'error'); setData({ items: [], total: 0 }) })
      return
    }
    const p = new URLSearchParams(key)
    p.delete('archived'); p.delete('root'); p.delete('folder')
    const extra = Object.fromEntries(p)
    api<Page<Hit>>('/api/assets' + qs({ ...extra, archived: f.archived ? 1 : 0, root: f.root ? 1 : undefined, folder_id: f.folder || undefined, page: f.page, page_size: PAGE }))
      .then(setData).catch(e => { toast(errText(e), 'error'); setData({ items: [], total: 0 }) })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, similar, toast])
  const loadFolders = useCallback(() => api<{ items: AssetFolder[] }>('/api/asset-folders').then(r => setFolders(r.items || [])).catch(() => setFolders([])), [])
  const reload = () => { load(); loadFolders() }
  useEffect(() => { setData(null); load() }, [load])
  useEffect(() => { loadFolders() }, [loadFolders])
  useEffect(() => { const t = setTimeout(() => q !== f.q && set({ q }), 300); return () => clearTimeout(t) })
  useEffect(() => { setSelected(new Set()) }, [f.folder])

  // 右键发起的高清化 / AI 修改：盯着后台任务，全部结束后刷新
  const jobList = jobIds.map(id => jobs[id]).filter(Boolean) as Job[]
  const runningJobs = jobIds.length - jobList.filter(j => !jobRunning(j)).length
  useEffect(() => {
    if (!jobIds.length || jobList.length < jobIds.length || jobList.some(jobRunning)) return
    const failed = jobList.filter(j => j.status === 'failed')
    toast(failed.length ? `处理完成，${failed.length} 个任务失败：${failed[0].message}` : '处理完成，新版本已放进素材库', failed.length ? 'error' : 'ok')
    setJobIds([]); reload()
  }, [jobs]) // eslint-disable-line react-hooks/exhaustive-deps

  const items = data?.items || []
  const byId = useMemo(() => new Map(items.map(a => [a.id, a])), [items])
  const selList = [...selected]
  const toggle = (id: number, shift: boolean) => {
    const next = new Set(selected)
    if (shift && last.current !== null) {
      const ids = items.map(a => a.id)
      const [a, b] = [ids.indexOf(last.current), ids.indexOf(id)].sort((x, y) => x - y)
      if (a >= 0) ids.slice(a, b + 1).forEach(i => next.add(i))
    } else if (next.has(id)) next.delete(id)
    else next.add(id)
    last.current = id
    setSelected(next)
  }
  const allOnPage = items.length > 0 && items.every(a => selected.has(a.id))
  const clearSel = () => setSelected(new Set())
  const selectPage = () => setSelected(new Set(items.map(a => a.id)))

  const sources = f.source ? f.source.split(',') : []
  const tags = f.tag ? f.tag.split(',') : []
  const filtered = !!(f.q || f.source || f.kind || f.tag || f.code || f.shop_id || f.product_id || f.from || f.to || f.media)
  const productName = f.product_id ? items.find(a => String(a.product_id) === f.product_id)?.product?.name : ''
  const folderId = Number(f.folder) || null
  const curFolder = folderId ? folders.find(x => x.id === folderId) : undefined
  const path = folderPath(folders, folderId)
  const folderLabel = curFolder ? path.map(x => x.name).join(' / ') : '不放进文件夹'

  const canUpload = can('asset.upload')
  const canDelete = can('asset.delete_own') || can('asset.delete_any')
  const canTryOn = can('model.view') && can('asset.process') && canUpload && me?.role.scope !== 'shares'
  const canFolders = canUpload && me?.role.scope !== 'shares'

  const batch = async (action: string, extra: Record<string, unknown> = {}, ids = selList) => {
    try {
      const r = await api<{ items: { asset_id: number; ok?: boolean; error?: string }[] }>('/api/assets/batch', { body: { action, asset_ids: ids, ...extra } })
      const ok = r.items.filter(i => i.ok).length
      const bad = r.items.filter(i => i.error)
      toast(bad.length ? `完成 ${ok} 张，${bad.length} 张没做：${bad[0].error}` : `已完成 ${ok} 张`, bad.length ? 'error' : 'ok')
      clearSel(); reload()
    } catch (e) { toast(errText(e), 'error') }
  }
  const byProduct = (ids: number[]) => {
    const groups = new Map<number, number[]>()
    ids.forEach(id => { const a = byId.get(id); if (a) groups.set(a.product_id, [...(groups.get(a.product_id) || []), id]) })
    return [...groups.values()]
  }
  const aiTag = async (ids = selList) => {
    // 打标签按商品建任务，跨商品的拆开发
    try {
      await Promise.all(byProduct(ids).map(g => api('/api/assets/tag', { body: { asset_ids: g } })))
      toast(`已开始给 ${ids.length} 张打标签，完成后刷新就能看到`, 'ok'); clearSel()
    } catch (e) { toast(errText(e), 'error') }
  }
  // 处理接口一次只收同一款的图，跨款拆开发
  const runProcess = async (ids: number[], ops: ProcessOps, label: string) => {
    try {
      const got = await Promise.all(byProduct(ids).map(g => api<{ job: Job }>('/api/assets/process', { body: { asset_ids: g, ops } })))
      setJobIds(old => [...old, ...got.map(r => r.job.id)])
      toast(`已开始${label} ${ids.length} 张，处理完会自动出现新版本，原图保留`, 'ok')
    } catch (e) { toast(errText(e), 'error') }
  }
  const download = async (ids = selList) => {
    if (ids.length === 1) {
      const a = byId.get(ids[0])
      if (a) { const el = document.createElement('a'); el.href = a.url; el.download = a.orig_name || ''; el.click() }
      return
    }
    try { await downloadPost('/api/assets/download', { asset_ids: ids }, `图片_${ids.length}张.zip`) } catch (e) { toast(errText(e), 'error') }
  }
  const remove = (ids = selList) => confirm(ids.length === 1 ? `删除「${assetTitle(byId.get(ids[0]) || { id: ids[0], orig_name: '' })}」？不能恢复。` : `删除选中的 ${ids.length} 张？只会删你有权删的，不能恢复。`) && batch('delete', {}, ids)
  const process = (ids: number[]) => nav('/process?assets=' + ids.join(','))
  const tryOn = (ids: number[]) => {
    const chosen = ids.map(id => byId.get(id))
    if (ids.length > 2 || chosen.some(a => !a || a.kind !== 'photo' || a.product_id !== chosen[0]?.product_id)) { toast('请选择同一商品的 1–2 张素材照片', 'error'); return }
    nav('/tryon?assets=' + ids.join(','))
  }
  const view = (id: number) => {
    const i = items.findIndex(a => a.id === id)
    lightbox(items.map(a => ({ src: a.url, title: assetTitle(a), meta: `${a.product?.name || `商品 #${a.product_id}`} · ${a.width}×${a.height}`, download: '' })), i)
  }
  const openFolder = (id: string) => { setSimilar(null); set({ folder: id }) }

  // 文件夹操作
  const moveToFolder = (ids: number[], fid: number | null) => batch('folder', { folder_id: fid }, ids)
  const createFolder = async (name: string, parent: number | null, moveIds?: number[]) => {
    try {
      const r = await api<{ id: number }>('/api/asset-folders', { body: { name, parent_id: parent } })
      setNameDlg(null)
      if (moveIds?.length) await moveToFolder(moveIds, r.id)
      else { toast(`已新建文件夹「${name}」`, 'ok'); loadFolders() }
    } catch (e) { toast(errText(e), 'error') }
  }
  const patchFolder = async (fo: AssetFolder, body: Record<string, unknown>) => {
    try { await api(`/api/asset-folders/${fo.id}`, { method: 'PATCH', body }); setNameDlg(null); loadFolders() }
    catch (e) { toast(errText(e), 'error') }
  }
  const deleteFolder = async (fo: AssetFolder) => {
    const subs = folders.filter(x => x.id !== fo.id && folderPath(folders, x.id).some(p => p.id === fo.id)).length
    if (!confirm(`删除文件夹「${fo.name}」${subs ? `和里面的 ${subs} 个子文件夹` : ''}？\n里面的图片不会删除，会变成「未归类」。`)) return
    try {
      const r = await api<{ unfiled: number }>(`/api/asset-folders/${fo.id}`, { method: 'DELETE' })
      toast(`已删除文件夹${r?.unfiled ? `，${r.unfiled} 张图移到未归类` : ''}`, 'ok')
      if (folderId && path.some(p => p.id === fo.id)) set({ folder: fo.parent_id ? String(fo.parent_id) : '' })
      reload()
    } catch (e) { toast(errText(e), 'error') }
  }
  const renameAsset = async (a: Asset, title: string) => {
    try { await api(`/api/assets/${a.id}`, { method: 'PATCH', body: { title } }); setNameDlg(null); toast('已重命名', 'ok'); load() }
    catch (e) { toast(errText(e), 'error') }
  }

  // 右键菜单
  const folderTargets = (ids: number[]): MenuEntry[] => {
    const inFolder = ids.map(i => byId.get(i)?.folder_id).filter(Boolean)
    return menuOf([
      { label: '新建文件夹并移入…', icon: <FolderPlus />, onClick: () => setNameDlg({ mode: 'folder', parent: folderId, moveIds: ids }) },
      { label: '移出文件夹', icon: <FolderX />, disabled: !inFolder.length, onClick: () => moveToFolder(ids, null) },
      folders.length > 0 && 'sep',
      ...flatFolders(folders).map(({ f: fo, depth }) => ({ label: <span style={{ paddingLeft: depth * 12 }}>{fo.name}</span>, icon: <Folder />, disabled: ids.every(i => byId.get(i)?.folder_id === fo.id), onClick: () => moveToFolder(ids, fo.id) }))
    ])
  }
  const itemMenu = (e: RMouseEvent, a: Hit) => {
    const ids = selected.has(a.id) ? selList : [a.id]
    if (!selected.has(a.id)) { setSelected(new Set([a.id])); last.current = a.id }
    const one = ids.length === 1
    const n = one ? '' : ` ${ids.length} 张`
    const allArchived = ids.every(i => byId.get(i)?.archived)
    menu.open(e, menuOf([
      one && { label: '查看大图', icon: <Maximize2 />, hint: '双击', onClick: () => view(a.id) },
      one && { label: '详情', icon: <PanelRight />, onClick: () => setActive(a.id) },
      one && canUpload && { label: '重命名', icon: <Pencil />, hint: 'F2', onClick: () => setNameDlg({ mode: 'rename-asset', asset: a }) },
      'sep',
      can('asset.upscale') && { label: '高清化' + n, icon: <Sparkles />, children: [2048, 3000, 4096].map(s => ({ label: `长边 ${s}`, hint: s === 3000 ? '常用' : '', onClick: () => runProcess(ids, { upscale: { target_long_side: s } }, '高清化') })) },
      can('asset.process') && { label: 'AI 修改' + n, icon: <Wand2 />, children: [
        { heading: '轻度（不改商品）' }, ...quickOps.filter(o => !o.heavy).map(o => ({ label: o.label, onClick: () => runProcess(ids, { ai: { quick: [o.key] } }, o.label) })),
        { heading: '改图' }, ...quickOps.filter(o => o.heavy).map(o => ({ label: o.label, onClick: () => runProcess(ids, { ai: { quick: [o.key] } }, o.label) }))
      ] },
      canUpload && { label: '在处理台修改…', icon: <SlidersHorizontal />, onClick: () => process(ids) },
      canTryOn && ids.length <= 2 && { label: '试穿试戴', icon: <Shirt />, onClick: () => tryOn(ids) },
      one && !similar && { label: '找相似', icon: <ScanSearch />, onClick: () => { setSimilar(a); clearSel() } },
      'sep',
      canFolders && { label: '移到文件夹' + n, icon: <FolderInput />, children: folderTargets(ids) },
      can('product.edit') && { label: '移到别的款…', icon: <Package />, onClick: () => { setSelected(new Set(ids)); setDialog('move') } },
      canUpload && { label: '改标签…', icon: <Tags />, onClick: () => { setSelected(new Set(ids)); setDialog('tag_add') } },
      canUpload && { label: 'AI 打标签', icon: <Tags />, onClick: () => aiTag(ids) },
      'sep',
      (one || can('export.run')) && { label: one ? '下载' : `下载${n}（zip）`, icon: <Download />, onClick: () => download(ids) },
      canUpload && { label: (allArchived ? '取消归档' : '归档') + n, icon: allArchived ? <ArchiveRestore /> : <Archive />, onClick: () => batch(allArchived ? 'unarchive' : 'archive', {}, ids) },
      canDelete && { label: '删除' + n, icon: <Trash2 />, danger: true, hint: 'Del', onClick: () => remove(ids) }
    ]))
  }
  const blankMenu = (e: RMouseEvent) => {
    if (similar) return
    clearSel()
    menu.open(e, menuOf([
      canUpload && { label: `上传图片到${curFolder ? `「${curFolder.name}」` : '素材库'}…`, icon: <Upload />, onClick: () => setUpload([]) },
      canFolders && { label: curFolder ? '新建子文件夹' : '新建文件夹', icon: <FolderPlus />, onClick: () => setNameDlg({ mode: 'folder', parent: folderId }) },
      'sep',
      items.length > 0 && { label: '全选本页', icon: <CheckSquare />, hint: 'Ctrl+A', onClick: selectPage },
      { label: '刷新', icon: <RefreshCw />, onClick: reload }
    ]))
  }
  const folderMenu = (e: RMouseEvent, fo: AssetFolder | null) => {
    if (!canFolders) { e.preventDefault(); return }
    if (!fo) { menu.open(e, [{ label: '新建文件夹', icon: <FolderPlus />, onClick: () => setNameDlg({ mode: 'folder', parent: null }) }]); return }
    const banned = new Set(folders.filter(x => folderPath(folders, x.id).some(p => p.id === fo.id)).map(x => x.id))
    menu.open(e, menuOf([
      { label: '打开', icon: <FolderOpen />, onClick: () => openFolder(String(fo.id)) },
      { label: '上传图片到这里…', icon: <Upload />, onClick: () => { openFolder(String(fo.id)); setUpload([]) } },
      { label: '新建子文件夹', icon: <FolderPlus />, onClick: () => setNameDlg({ mode: 'folder', parent: fo.id }) },
      'sep',
      { label: '重命名', icon: <Pencil />, onClick: () => setNameDlg({ mode: 'rename-folder', folder: fo }) },
      { label: '移到', icon: <FolderInput />, children: menuOf([
        { label: '顶层', icon: <Layers />, disabled: !fo.parent_id, onClick: () => patchFolder(fo, { parent_id: null }) },
        'sep',
        ...flatFolders(folders).map(({ f: x, depth }) => ({ label: <span style={{ paddingLeft: depth * 12 }}>{x.name}</span>, icon: <Folder />, disabled: banned.has(x.id) || x.id === fo.parent_id, onClick: () => patchFolder(fo, { parent_id: x.id }) }))
      ]) },
      'sep',
      { label: '删除文件夹', icon: <Trash2 />, danger: true, onClick: () => deleteFolder(fo) }
    ]))
  }

  // 键盘：Ctrl+A 全选本页、Del 删除、F2 重命名、Esc 取消选择
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement
      if (t.closest('input, textarea, select, [contenteditable="true"]') || document.querySelector('.mask, .lightbox, .ctx-menu')) return
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'a' && items.length) { e.preventDefault(); selectPage() }
      else if (e.key === 'Delete' && selected.size && canDelete) remove(selList)
      else if (e.key === 'F2' && selected.size === 1 && canUpload) { const a = byId.get(selList[0]); if (a) { e.preventDefault(); setNameDlg({ mode: 'rename-asset', asset: a }) } }
      else if (e.key === 'Escape' && selected.size) clearSel()
    }
    addEventListener('keydown', onKey)
    return () => removeEventListener('keydown', onKey)
  })

  // 素材拖到文件夹；从电脑拖图片进来直接上传
  const dragStart = (e: DragEvent, a: Hit) => {
    const ids = selected.has(a.id) ? selList : [a.id]
    e.dataTransfer.setData(DRAG_ASSETS, JSON.stringify(ids))
    e.dataTransfer.effectAllowed = 'move'
    setDragging(ids)
  }
  const isFiles = (e: DragEvent) => e.dataTransfer.types.includes('Files') && !e.dataTransfer.types.includes(DRAG_ASSETS)

  const shops = me?.shops || []
  const pages = data ? Math.max(1, Math.ceil(data.total / PAGE)) : 1
  const subfolders = folderId && !similar ? childrenOf(folders, folderId) : []
  const home = <a className="link" onClick={() => { setSimilar(null); set({ folder: '' }) }}>全部图片和视频</a>
  const crumbs = similar ? [home, `与「${assetTitle(similar)}」相似`]
    : f.folder === '0' ? [home, '未归类']
      : curFolder ? [home, ...path.slice(0, -1).map(x => <a className="link" onClick={() => openFolder(String(x.id))}>{x.name}</a>), curFolder.name]
        : ['商品库']
  const blankTarget = (t: EventTarget) => { const el = t as HTMLElement; return el.classList.contains('scroll') || el.classList.contains('lib-grid') || el.classList.contains('lib-subfolders') || !!el.closest('.empty') }

  return (
    <>
      <Topbar crumbs={crumbs}>
        <LibrarySwitch />
        {similar && <button className="btn btn-ghost btn-sm" onClick={() => setSimilar(null)}><ArrowLeft />返回素材库</button>}
        {!similar && canFolders && <button className="btn btn-secondary btn-sm" onClick={() => setNameDlg({ mode: 'folder', parent: folderId })}><FolderPlus />{curFolder ? '新建子文件夹' : '新建文件夹'}</button>}
        {!similar && canUpload && <button className="btn btn-primary btn-sm" onClick={() => setUpload([])}><Upload />上传</button>}
      </Topbar>
      <div className="lib">
        <aside className="sidebar lib-filters">
          <div className="nav-group">
            <a className={'nav-item' + (!f.root && !f.archived && !f.folder && !f.media ? ' active' : '')} onClick={() => set({ root: false, archived: false, folder: '', media: '' })}><Images />全部</a>
            <a className={'nav-item' + (f.media === 'video' && !f.archived ? ' active' : '')} onClick={() => set({ media: 'video', root: false, archived: false })}><Film />只看视频</a>
            <a className={'nav-item' + (f.root ? ' active' : '')} onClick={() => set({ root: true, archived: false })} title="只看上传的原图，不看修图、AI 出图这些处理出来的版本"><Layers />只看原图</a>
            <a className={'nav-item' + (f.archived ? ' active' : '')} onClick={() => set({ archived: true, root: false })}><Archive />已归档</a>
          </div>
          {me?.role.scope !== 'shares' && <div className="nav-group">
            <div className="nav-label nav-label-row">文件夹{canFolders && <button className="btn btn-ghost btn-icon btn-sm" title="新建文件夹" onClick={() => setNameDlg({ mode: 'folder', parent: null })}><FolderPlus /></button>}</div>
            <FolderTree folders={folders} current={f.folder} onOpen={openFolder} onMenu={folderMenu}
              onDropAssets={(fid, ids) => { if (canFolders) moveToFolder(ids, fid) }} />
          </div>}
          <div className="nav-group">
            <div className="nav-label">来源</div>
            {Object.entries(sourceName).map(([k, v]) => (
              <label key={k} className="filter-opt">
                <input type="checkbox" className="check" checked={sources.includes(k)}
                  onChange={e => set({ source: (e.target.checked ? [...sources, k] : sources.filter(s => s !== k)).join(',') })} />
                <Pill kind={v.pill} dot={false}>{v.label}</Pill>
              </label>
            ))}
          </div>
          <div className="nav-group">
            <div className="nav-label">类型</div>
            <div className="segmented block">
              {[['', '全部'], ['photo', '实拍和处理'], ['output', 'AI 成品']].map(([k, t]) => <button key={k} className={f.kind === k ? 'active' : ''} onClick={() => set({ kind: k })}>{t}</button>)}
            </div>
            <label className="filter-opt" title="被重画、改字替换掉的旧图，和人工弃用的图，默认不显示">
              <input type="checkbox" className="check" checked={f.stale === 'all'} onChange={e => set({ stale: e.target.checked ? 'all' : '' })} />显示旧版本和弃用的图
            </label>
          </div>
          {shops.length > 1 && (
            <div className="nav-group">
              <div className="nav-label">店铺</div>
              <select className="input" value={f.shop_id} onChange={e => set({ shop_id: e.target.value })}>
                <option value="">全部店铺</option>
                {shops.map(s => <option key={s.id} value={s.id}>{s.name}</option>)}
              </select>
            </div>
          )}
          <div className="nav-group">
            <div className="nav-label">照片类型</div>
            <select className="input" value={f.code} onChange={e => set({ code: e.target.value })}>
              <option value="">全部</option>
              {codeGroups.map(g => <optgroup key={g.group} label={g.group}>
                <option value={g.prefix}>{g.group}（全部）</option>
                {g.codes.map(c => <option key={c.code} value={c.code}>{c.name}</option>)}
              </optgroup>)}
            </select>
          </div>
          <div className="nav-group">
            <div className="nav-label">标签</div>
            <TagInput placeholder="输入后回车，要求都包含" tags={tags} onChange={t => set({ tag: t.join(',') })} />
          </div>
          <div className="nav-group">
            <div className="nav-label">上传日期</div>
            <div className="date-range">
              <input type="date" className="input mono" value={f.from} onChange={e => set({ from: e.target.value })} />
              <span className="t4">–</span>
              <input type="date" className="input mono" value={f.to} onChange={e => set({ to: e.target.value })} />
            </div>
          </div>
          {filtered && <button className="btn btn-ghost btn-sm" onClick={() => { setQ(''); const n = new URLSearchParams(); if (f.folder) n.set('folder', f.folder); setSp(n, { replace: true }) }}><X />清空筛选</button>}
        </aside>

        <div className="lib-main"
          onDragOver={e => { if (canUpload && !similar && isFiles(e)) { e.preventDefault(); setFileOver(true) } }}
          onDragLeave={e => { if (!e.currentTarget.contains(e.relatedTarget as Node)) setFileOver(false) }}
          onDrop={e => { if (!isFiles(e)) return; e.preventDefault(); setFileOver(false); if (canUpload && !similar) setUpload(Array.from(e.dataTransfer.files)) }}>
          {fileOver && <div className="lib-dropzone">松开上传到{curFolder ? `「${curFolder.name}」` : '素材库'}</div>}
          <div className="lib-toolbar">
            {!similar && <label className="field" style={{ width: 260 }}><Search /><input placeholder="搜文件名、标题、备注、标签、款名" value={q} onChange={e => setQ(e.target.value)} /></label>}
            {f.product_id && !similar && <span className="tag">款：{productName || `#${f.product_id}`}<button className="tag-x" onClick={() => set({ product_id: '' })} aria-label="取消"><X /></button></span>}
            <span className="t3 fs-12">{data ? (similar ? `找到 ${data.total} 张相似图（越靠前越像）` : `共 ${data.total} 张`) : ''}</span>
            {runningJobs > 0 && <span className="inline t3 fs-12"><Loader2 className="spin" size={14} />{runningJobs} 个处理任务进行中</span>}
            <span className="spacer" />
            {items.length > 0 && <label className="filter-opt"><input type="checkbox" className="check" checked={allOnPage}
              onChange={e => { const n = new Set(selected); items.forEach(a => e.target.checked ? n.add(a.id) : n.delete(a.id)); setSelected(n) }} />全选本页</label>}
            {!similar && <div className="segmented">
              <button className={f.sort === 'new' ? 'active' : ''} onClick={() => set({ sort: 'new' })}>最新</button>
              <button className={f.sort === 'old' ? 'active' : ''} onClick={() => set({ sort: 'old' })}>最早</button>
            </div>}
          </div>
          <div className="scroll" onContextMenu={e => { if (blankTarget(e.target)) blankMenu(e) }}
            onClick={e => { if (blankTarget(e.target) && !(e.target as HTMLElement).closest('.empty')) clearSel() }}>
            {subfolders.length > 0 && <div className="lib-subfolders">
              {subfolders.map(x => <FolderTile key={x.id} f={x} onOpen={() => openFolder(String(x.id))} onMenu={e => folderMenu(e, x)} onDrop={ids => { if (canFolders) moveToFolder(ids, x.id) }} />)}
            </div>}
            {!data ? <Loading /> : items.length === 0 ? (
              <Empty title={similar ? '没有找到相似的图' : filtered ? '没有符合条件的图' : f.archived ? '没有归档的图' : curFolder ? '这个文件夹里没有图' : f.folder === '0' ? '没有未归类的图' : '素材库还是空的'}
                desc={similar ? '这里只找得出几乎一样的图，比如重复上传的、同一张图的其他版本；换个角度重拍的不算。' : filtered ? '换个筛选条件试试。' : curFolder ? '把图拖到左边的文件夹上，或者右键图片「移到文件夹」；也可以直接把电脑里的图拖进来上传。' : '点右上角「上传」，或把电脑里的图直接拖进来。'}>
                {!similar && !filtered && canUpload && <button className="btn btn-secondary btn-sm" onClick={() => setUpload([])}><Upload />上传图片或视频</button>}
              </Empty>
            ) : (
              <div className="lib-grid">
                {items.map(a => {
                  const src = sourceName[a.effective_source || a.source_override || a.source] || sourceName.original
                  const sel = selected.has(a.id)
                  return (
                    <div key={a.id} className={'lib-item' + (sel ? ' selected' : '') + (active === a.id ? ' active' : '') + (dragging.includes(a.id) ? ' dragging' : '')}
                      onClick={e => { if (e.ctrlKey || e.metaKey || e.shiftKey) toggle(a.id, e.shiftKey); else setActive(a.id) }}
                      onDoubleClick={() => view(a.id)} onContextMenu={e => itemMenu(e, a)}
                      draggable={canFolders && !similar} onDragStart={e => dragStart(e, a)} onDragEnd={() => setDragging([])}>
                      <div className="lib-img">
                        {a.media === 'video'
                          ? (a.poster_url ? <img src={a.poster_url} alt="" loading="lazy" draggable={false} /> : <span className="lib-video-blank"><Film /></span>)
                          : <img src={a.thumb_url || a.url + '?w=240'} alt="" loading="lazy" draggable={false} />}
                        {a.media === 'video' && <span className="lib-video-badge mono"><Play />{fmtDuration(a.duration_ms) || '视频'}</span>}
                        <input type="checkbox" className="check lib-check" checked={sel} aria-label="选择"
                          onClick={e => { e.stopPropagation(); toggle(a.id, e.shiftKey) }} onChange={() => {}} />
                        {src.label !== '原图' && <span className="lib-src"><Pill kind={src.pill} dot={false}>{src.label}</Pill></span>}
                        {canUpload && a.media !== 'video' && <button className="lib-hover btn btn-sm" onClick={e => { e.stopPropagation(); process([a.id]) }}><Wand2 />处理</button>}
                        {a.stale && <span className="lib-stale">旧版本</span>}
                        {a.kind !== 'output' && (a.version_count || 1) > 1 && <span className="lib-ver mono" title="这张原图修过几个版本（含原图）">{a.version_count} 版</span>}
                        {a.distance !== undefined && <span className="lib-ver mono" title="差异值，0 表示几乎一样">差 {a.distance}</span>}
                      </div>
                      <div className="lib-meta">
                        <div className="row" style={{ gap: 6 }}>
                          {a.code ? <span className="code" title={`拍摄编号 ${a.code}`}>{codeName(a.code)}</span> : null}
                          <span className="ellipsis cell-title" style={{ fontSize: 'var(--fs-12)' }}>{assetTitle(a)}</span>
                        </div>
                        <div className="ellipsis t3 fs-12">{a.product?.name || `商品 #${a.product_id}`}{a.width > 0 && <span className="t4 mono"> · {a.width}×{a.height}</span>}</div>
                      </div>
                    </div>
                  )
                })}
              </div>
            )}
            {data && !similar && pages > 1 && (
              <div className="pager lib-pager">
                <button className="btn btn-secondary btn-sm" disabled={f.page <= 1} onClick={() => set({ page: f.page - 1 })}>上一页</button>
                <span className="mono">{f.page} / {pages}</span>
                <button className="btn btn-secondary btn-sm" disabled={f.page >= pages} onClick={() => set({ page: f.page + 1 })}>下一页</button>
              </div>
            )}
          </div>
          {selected.size > 0 && (
            <div className="bulkbar lib-bulk">
              <span className="count">已选 <span className="mono">{selected.size}</span> 张</span>
              <button className="btn btn-ghost btn-sm" onClick={clearSel}>取消选择</button>
              <div className="actions">
                {canUpload && <>
                  <button className="btn btn-secondary btn-sm" onClick={() => setDialog('tag_add')}><Tags />改标签</button>
                  <button className="btn btn-secondary btn-sm" onClick={() => aiTag()}><Tags />AI 打标签</button>
                  <button className="btn btn-secondary btn-sm" onClick={() => batch(f.archived ? 'unarchive' : 'archive')}><Archive />{f.archived ? '取消归档' : '归档'}</button>
                </>}
                {canFolders && <button className="btn btn-secondary btn-sm" onClick={e => { const r = e.currentTarget.getBoundingClientRect(); menu.open({ clientX: r.left, clientY: r.top, preventDefault() {}, stopPropagation() {} }, folderTargets(selList)) }}><Folder />移到文件夹</button>}
                {can('product.edit') && <button className="btn btn-secondary btn-sm" onClick={() => setDialog('move')}><FolderInput />移到别的款</button>}
                {can('export.run') && <button className="btn btn-secondary btn-sm" onClick={() => download()}><Download />下载 {selected.size} 张</button>}
                {canDelete && <button className="btn btn-secondary btn-sm" onClick={() => remove()}><Trash2 />删除</button>}
                {canTryOn && <button className="btn btn-secondary btn-sm" disabled={selected.size > 2} onClick={() => tryOn(selList)}><Shirt />试穿试戴</button>}
                {canUpload && <button className="btn btn-primary btn-sm" onClick={() => process(selList)}><Wand2 />处理 {selected.size} 张</button>}
              </div>
            </div>
          )}
        </div>

        {active !== null && (
          <AssetInspector id={active} onClose={() => setActive(null)} onChanged={reload}
            folders={canFolders ? folders : undefined}
            onProcess={id => process([id])}
            onTryOn={id => nav('/tryon?assets=' + id)}
            onSimilar={a => { setSimilar(a); clearSel() }}
            onOpenProduct={pid => { setSimilar(null); set({ product_id: String(pid), folder: '' }) }}
            onSelect={setActive} />
        )}
      </div>
      {menu.node}

      {(dialog === 'tag_add' || dialog === 'tag_remove') && (
        <TagDialog mode={dialog} count={selected.size} onMode={setDialog} onClose={() => setDialog(null)}
          onSubmit={t => { setDialog(null); batch(dialog, { tags: t }) }} />
      )}
      {dialog === 'move' && <MoveDialog count={selected.size} onClose={() => setDialog(null)} onSubmit={pid => { setDialog(null); batch('move', { product_id: pid }) }} />}
      {nameDlg?.mode === 'folder' && <NameDialog title={nameDlg.moveIds ? `新建文件夹并移入 ${nameDlg.moveIds.length} 张` : nameDlg.parent ? `在「${folders.find(x => x.id === nameDlg.parent)?.name || ''}」里新建文件夹` : '新建文件夹'}
        label="文件夹名" hint="不超过 30 个字。文件夹全团队共用，删除文件夹不会删里面的图。" onClose={() => setNameDlg(null)}
        onSubmit={v => createFolder(v, nameDlg.parent, nameDlg.moveIds)} />}
      {nameDlg?.mode === 'rename-folder' && <NameDialog title="重命名文件夹" label="文件夹名" initial={nameDlg.folder.name} onClose={() => setNameDlg(null)}
        onSubmit={v => patchFolder(nameDlg.folder, { name: v })} />}
      {nameDlg?.mode === 'rename-asset' && <NameDialog title="重命名" label="显示名" max={60} initial={nameDlg.asset.title || nameDlg.asset.orig_name} hint={`原文件名：${nameDlg.asset.orig_name}，不会改动。`}
        onClose={() => setNameDlg(null)} onSubmit={v => renameAsset(nameDlg.asset, v)} />}
      {upload && <UploadDialog files={upload} folder={folderId} folderName={folderLabel}
        productId={Number(f.product_id) || undefined} onClose={() => setUpload(null)} onDone={() => { setUpload(null); reload() }} />}
    </>
  )
}

export function TagInput({ tags, onChange, placeholder }: { tags: string[]; onChange: (t: string[]) => void; placeholder?: string }) {
  const [v, setV] = useState('')
  const add = () => {
    const parts = v.split(/[,，、\s]+/).map(s => s.trim()).filter(Boolean)
    if (parts.length) onChange([...new Set([...tags, ...parts])])
    setV('')
  }
  return (
    <div className="tag-input">
      {tags.map(t => <span key={t} className="tag">{t}<button className="tag-x" onClick={() => onChange(tags.filter(x => x !== t))} aria-label={`去掉 ${t}`}><X /></button></span>)}
      <input value={v} placeholder={tags.length ? '' : placeholder} onChange={e => setV(e.target.value)}
        onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); add() } else if (e.key === 'Backspace' && !v && tags.length) onChange(tags.slice(0, -1)) }}
        onBlur={add} />
    </div>
  )
}

function TagDialog({ mode, count, onMode, onClose, onSubmit }: { mode: 'tag_add' | 'tag_remove'; count: number; onMode: (m: 'tag_add' | 'tag_remove') => void; onClose: () => void; onSubmit: (t: string[]) => void }) {
  const [tags, setTags] = useState<string[]>([])
  return (
    <Drawer title={`给 ${count} 张图改标签`} width={380} onClose={onClose} foot={<>
      <button className="btn btn-secondary" onClick={onClose}>取消</button>
      <button className="btn btn-primary" disabled={!tags.length} onClick={() => onSubmit(tags)}>{mode === 'tag_add' ? '加上' : '去掉'} {tags.length} 个标签</button>
    </>}>
      <div className="segmented block">
        <button className={mode === 'tag_add' ? 'active' : ''} onClick={() => onMode('tag_add')}>加标签</button>
        <button className={mode === 'tag_remove' ? 'active' : ''} onClick={() => onMode('tag_remove')}>去掉标签</button>
      </div>
      <Field label="标签" hint="可以一次输多个，用逗号或空格隔开，回车确认。每个不超过 10 个字。">
        <TagInput tags={tags} onChange={setTags} placeholder="如：上耳、礼盒" />
      </Field>
    </Drawer>
  )
}

function MoveDialog({ count, onClose, onSubmit }: { count: number; onClose: () => void; onSubmit: (pid: number) => void }) {
  const [list, setList] = useState<Product[] | null>(null)
  const [q, setQ] = useState('')
  const [pid, setPid] = useState<number | null>(null)
  useEffect(() => { api<Page<Product>>('/api/products' + qs({ q, page_size: 50 })).then(r => setList(r.items)).catch(() => setList([])) }, [q])
  return (
    <Drawer title={`把 ${count} 张图移到别的款`} width={420} onClose={onClose} foot={<>
      <button className="btn btn-secondary" onClick={onClose}>取消</button>
      <button className="btn btn-primary" disabled={!pid} onClick={() => pid && onSubmit(pid)}>移过去</button>
    </>}>
      <div className="t3 fs-12">还占着拍摄编号的图不能移，会被跳过。移走后和原来那款的版本关系会断开。</div>
      <label className="field"><Search /><input autoFocus placeholder="搜款名" value={q} onChange={e => setQ(e.target.value)} /></label>
      {!list ? <Loading /> : list.length === 0 ? <Empty title="没有找到" /> : (
        <div className="list">{list.map(p => (
          <label key={p.id} className="list-item" style={{ cursor: 'pointer' }}>
            <input type="radio" className="radio" checked={pid === p.id} onChange={() => setPid(p.id)} />
            {p.cover_url ? <img className="cover" src={p.cover_url} alt="" /> : <span className="cover"><ImageOff /></span>}
            <div style={{ minWidth: 0 }}><div className="title ellipsis">{p.name}</div><div className="meta mono">#{p.id}</div></div>
          </label>
        ))}</div>
      )}
    </Drawer>
  )
}

