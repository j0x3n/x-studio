import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { Search, Images, Archive, Layers, X, Wand2, Tags, Download, FolderInput, Trash2, ArrowLeft, ImageOff } from 'lucide-react'
import { api, downloadPost, qs, type Page } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import type { Asset, Product } from '../lib/types'
import { Topbar, Pill, Loading, Empty, Drawer, Field } from '../components/ui'
import { AssetInspector } from '../components/AssetInspector'
import { sourceName, assetTitle } from '../lib/format'

const PAGE = 60
type Hit = Asset & { distance?: number }

// 素材库：左边筛选，中间网格，点开一张在右边看详情；勾选后底部出批量操作
export function Assets() {
  const { me, can } = useAuth()
  const toast = useToast()
  const nav = useNavigate()
  const [sp, setSp] = useSearchParams()
  const f = {
    q: sp.get('q') || '', source: sp.get('source') || '', kind: sp.get('kind') || '', tag: sp.get('tag') || '',
    code: sp.get('code') || '', shop_id: sp.get('shop_id') || '', product_id: sp.get('product_id') || '',
    from: sp.get('from') || '', to: sp.get('to') || '', sort: sp.get('sort') || 'new',
    root: sp.get('root') === '1', archived: sp.get('archived') === '1', page: Number(sp.get('page') || 1)
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
  const [q, setQ] = useState(f.q)
  const [selected, setSelected] = useState<Set<number>>(new Set())
  const [active, setActive] = useState<number | null>(null)
  const [similar, setSimilar] = useState<Asset | null>(null)
  const [dialog, setDialog] = useState<'tag_add' | 'tag_remove' | 'move' | null>(null)
  const last = useRef<number | null>(null)
  const key = sp.toString()

  const load = useCallback(() => {
    if (similar) {
      api<Page<Hit>>(`/api/assets/${similar.id}/similar`, { method: 'POST' }).then(setData).catch(e => { toast(errText(e), 'error'); setData({ items: [], total: 0 }) })
      return
    }
    const p = new URLSearchParams(key)
    p.delete('archived'); p.delete('root')
    const extra = Object.fromEntries(p)
    api<Page<Hit>>('/api/assets' + qs({ ...extra, archived: f.archived ? 1 : 0, root: f.root ? 1 : undefined, page: f.page, page_size: PAGE }))
      .then(setData).catch(e => { toast(errText(e), 'error'); setData({ items: [], total: 0 }) })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, similar, toast])
  useEffect(() => { setData(null); load() }, [load])
  useEffect(() => { const t = setTimeout(() => q !== f.q && set({ q }), 300); return () => clearTimeout(t) })

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

  const sources = f.source ? f.source.split(',') : []
  const tags = f.tag ? f.tag.split(',') : []
  const filtered = !!(f.q || f.source || f.kind || f.tag || f.code || f.shop_id || f.product_id || f.from || f.to)
  const productName = f.product_id ? items.find(a => String(a.product_id) === f.product_id)?.product?.name : ''

  const batch = async (action: string, extra: Record<string, unknown> = {}) => {
    try {
      const r = await api<{ items: { asset_id: number; ok?: boolean; error?: string }[] }>('/api/assets/batch', { body: { action, asset_ids: selList, ...extra } })
      const ok = r.items.filter(i => i.ok).length
      const bad = r.items.filter(i => i.error)
      toast(bad.length ? `完成 ${ok} 张，${bad.length} 张没做：${bad[0].error}` : `已完成 ${ok} 张`, bad.length ? 'error' : 'ok')
      clearSel(); load()
    } catch (e) { toast(errText(e), 'error') }
  }
  const aiTag = async () => {
    // 打标签按商品建任务，跨商品的拆开发
    const groups = new Map<number, number[]>()
    selList.forEach(id => { const a = byId.get(id); if (a) groups.set(a.product_id, [...(groups.get(a.product_id) || []), id]) })
    try {
      await Promise.all([...groups.values()].map(ids => api('/api/assets/tag', { body: { asset_ids: ids } })))
      toast(`已开始给 ${selList.length} 张打标签，完成后刷新就能看到`, 'ok'); clearSel()
    } catch (e) { toast(errText(e), 'error') }
  }
  const download = async () => {
    try { await downloadPost('/api/assets/download', { asset_ids: selList }, `图片_${selList.length}张.zip`) } catch (e) { toast(errText(e), 'error') }
  }
  const remove = () => confirm(`删除选中的 ${selList.length} 张？只会删你有权删的，不能恢复。`) && batch('delete')
  const process = (ids: number[]) => nav('/process?assets=' + ids.join(','))

  const shops = me?.shops || []
  const pages = data ? Math.max(1, Math.ceil(data.total / PAGE)) : 1

  return (
    <>
      <Topbar crumbs={similar ? [<a className="link" onClick={() => setSimilar(null)}>素材库</a>, `与「${assetTitle(similar)}」相似`] : ['素材库']}>
        {similar && <button className="btn btn-ghost btn-sm" onClick={() => setSimilar(null)}><ArrowLeft />返回素材库</button>}
      </Topbar>
      <div className="lib">
        <aside className="sidebar lib-filters">
          <div className="nav-group">
            <a className={'nav-item' + (!f.root && !f.archived ? ' active' : '')} onClick={() => set({ root: false, archived: false })}><Images />全部图片</a>
            <a className={'nav-item' + (f.root ? ' active' : '')} onClick={() => set({ root: true, archived: false })}><Layers />只看原图</a>
            <a className={'nav-item' + (f.archived ? ' active' : '')} onClick={() => set({ archived: true, root: false })}><Archive />已归档</a>
          </div>
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
              {[['', '全部'], ['photo', '素材'], ['output', '成品']].map(([k, t]) => <button key={k} className={f.kind === k ? 'active' : ''} onClick={() => set({ kind: k })}>{t}</button>)}
            </div>
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
            <div className="nav-label">拍摄编号</div>
            <input className="input mono" placeholder="B 或 B2" value={f.code} onChange={e => set({ code: e.target.value.trim().toUpperCase() })} />
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
          {filtered && <button className="btn btn-ghost btn-sm" onClick={() => { setQ(''); setSp(new URLSearchParams(), { replace: true }) }}><X />清空筛选</button>}
        </aside>

        <div className="lib-main">
          <div className="lib-toolbar">
            {!similar && <label className="field" style={{ width: 260 }}><Search /><input placeholder="搜文件名、标题、备注、标签、款名" value={q} onChange={e => setQ(e.target.value)} /></label>}
            {f.product_id && !similar && <span className="tag">款：{productName || `#${f.product_id}`}<button className="tag-x" onClick={() => set({ product_id: '' })} aria-label="取消"><X /></button></span>}
            <span className="t3 fs-12">{data ? (similar ? `找到 ${data.total} 张相似图（越靠前越像）` : `共 ${data.total} 张`) : ''}</span>
            <span className="spacer" />
            {items.length > 0 && <label className="filter-opt"><input type="checkbox" className="check" checked={allOnPage}
              onChange={e => { const n = new Set(selected); items.forEach(a => e.target.checked ? n.add(a.id) : n.delete(a.id)); setSelected(n) }} />全选本页</label>}
            {!similar && <div className="segmented">
              <button className={f.sort === 'new' ? 'active' : ''} onClick={() => set({ sort: 'new' })}>最新</button>
              <button className={f.sort === 'old' ? 'active' : ''} onClick={() => set({ sort: 'old' })}>最早</button>
            </div>}
          </div>
          <div className="scroll">
            {!data ? <Loading /> : items.length === 0 ? (
              <Empty title={similar ? '没有找到相似的图' : filtered ? '没有符合条件的图' : f.archived ? '没有归档的图' : '素材库还是空的'}
                desc={similar ? '这里只找得出几乎一样的图，比如重复上传的、同一张图的其他版本；换个角度重拍的不算。' : filtered ? '换个筛选条件试试。' : '在商品页上传实拍图后，会出现在这里。'} />
            ) : (
              <div className="lib-grid">
                {items.map(a => {
                  const src = sourceName[a.effective_source || a.source_override || a.source] || sourceName.original
                  const sel = selected.has(a.id)
                  return (
                    <div key={a.id} className={'lib-item' + (sel ? ' selected' : '') + (active === a.id ? ' active' : '')} onClick={() => setActive(a.id)}>
                      <div className="lib-img">
                        <img src={a.thumb_url || a.url + '?w=240'} alt="" loading="lazy" />
                        <input type="checkbox" className="check lib-check" checked={sel} aria-label="选择"
                          onClick={e => { e.stopPropagation(); toggle(a.id, e.shiftKey) }} onChange={() => {}} />
                        {src.label !== '实拍' && <span className="lib-src"><Pill kind={src.pill} dot={false}>{src.label}</Pill></span>}
                        {can('asset.upload') && <button className="lib-hover btn btn-sm" onClick={e => { e.stopPropagation(); process([a.id]) }}><Wand2 />处理</button>}
                        {(a.version_count || 1) > 1 && <span className="lib-ver mono" title="同一张原图的版本数">{a.version_count} 版</span>}
                        {a.distance !== undefined && <span className="lib-ver mono" title="差异值，0 表示几乎一样">差 {a.distance}</span>}
                      </div>
                      <div className="lib-meta">
                        <div className="row" style={{ gap: 6 }}>
                          {a.code ? <span className="code mono">{a.code}</span> : null}
                          <span className="ellipsis cell-title" style={{ fontSize: 'var(--fs-12)' }}>{assetTitle(a)}</span>
                        </div>
                        <div className="ellipsis t3 fs-12">{a.product?.name || `商品 #${a.product_id}`}<span className="t4 mono"> · {a.width}×{a.height}</span></div>
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
                {can('asset.upload') && <>
                  <button className="btn btn-secondary btn-sm" onClick={() => setDialog('tag_add')}><Tags />加标签</button>
                  <button className="btn btn-secondary btn-sm" onClick={aiTag}><Tags />AI 打标签</button>
                  <button className="btn btn-secondary btn-sm" onClick={() => batch(f.archived ? 'unarchive' : 'archive')}><Archive />{f.archived ? '取消归档' : '归档'}</button>
                </>}
                {can('product.edit') && <button className="btn btn-secondary btn-sm" onClick={() => setDialog('move')}><FolderInput />移到别的款</button>}
                {can('export.run') && <button className="btn btn-secondary btn-sm" onClick={download}><Download />下载 {selected.size} 张</button>}
                {(can('asset.delete_own') || can('asset.delete_any')) && <button className="btn btn-secondary btn-sm" onClick={remove}><Trash2 />删除</button>}
                {can('asset.upload') && <button className="btn btn-primary btn-sm" onClick={() => process(selList)}><Wand2 />处理 {selected.size} 张</button>}
              </div>
            </div>
          )}
        </div>

        {active !== null && (
          <AssetInspector id={active} onClose={() => setActive(null)} onChanged={load}
            onProcess={id => process([id])}
            onSimilar={a => { setSimilar(a); clearSel() }}
            onOpenProduct={pid => { setSimilar(null); set({ product_id: String(pid) }) }}
            onSelect={setActive} />
        )}
      </div>

      {(dialog === 'tag_add' || dialog === 'tag_remove') && (
        <TagDialog mode={dialog} count={selected.size} onMode={setDialog} onClose={() => setDialog(null)}
          onSubmit={t => { setDialog(null); batch(dialog, { tags: t }) }} />
      )}
      {dialog === 'move' && <MoveDialog count={selected.size} onClose={() => setDialog(null)} onSubmit={pid => { setDialog(null); batch('move', { product_id: pid }) }} />}
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

