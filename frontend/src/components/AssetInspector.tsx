import { useCallback, useEffect, useState } from 'react'
import { X, Wand2, ScanSearch, Download, Archive, ArchiveRestore, Star, Package } from 'lucide-react'
import { api } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import type { Asset } from '../lib/types'
import { Pill, Loading } from './ui'
import { TagInput } from '../pages/Assets'
import { sourceName, assetTitle, fmtDate, opName, quickOps, imgUrl } from '../lib/format'

type UsedBy = { type: 'code'; product_id: number; code: string } | { type: 'listing'; id: number; shop_id: number }
type Detail = { asset: Asset; versions: Asset[]; used_by: UsedBy[] }

// 素材库右侧的详情栏：预览、基本信息、标签备注、版本链、处理记录、被谁用着
export function AssetInspector({ id, onClose, onChanged, onProcess, onSimilar, onOpenProduct, onSelect }: {
  id: number; onClose: () => void; onChanged: () => void; onProcess: (id: number) => void
  onSimilar: (a: Asset) => void; onOpenProduct: (pid: number) => void; onSelect: (id: number) => void
}) {
  const { me, can } = useAuth()
  const toast = useToast()
  const [d, setD] = useState<Detail | null>(null)
  const [title, setTitle] = useState('')
  const [note, setNote] = useState('')
  const load = useCallback(() => api<Detail>(`/api/assets/${id}`).then(r => { setD(r); setTitle(r.asset.title || ''); setNote(r.asset.note || '') }).catch(e => { toast(errText(e), 'error'); onClose() }), [id, toast, onClose])
  useEffect(() => { setD(null); load() }, [load])

  const patch = async (body: Record<string, unknown>, okMsg?: string) => {
    try { await api(`/api/assets/${id}`, { method: 'PATCH', body }); if (okMsg) toast(okMsg, 'ok'); load(); onChanged() }
    catch (e) { toast(errText(e), 'error'); load() }
  }
  // 把拍摄编号从当前占着的版本转给这一张：先腾出来再设上（一键出图在跑时后端会拒绝）
  const makeCurrent = async (holder: Asset) => {
    if (!d) return
    try {
      await api(`/api/assets/${holder.id}`, { method: 'PATCH', body: { code: '' } })
      try { await api(`/api/assets/${id}`, { method: 'PATCH', body: { code: holder.code } }) }
      catch (e) { await api(`/api/assets/${holder.id}`, { method: 'PATCH', body: { code: holder.code } }).catch(() => {}); throw e }
      toast(`${holder.code} 改用这张了，下次出图生效`, 'ok'); load(); onChanged()
    } catch (e) { toast(errText(e), 'error'); load() }
  }

  if (!d) return <aside className="inspector lib-inspector"><Loading /></aside>
  const a = d.asset
  const src = sourceName[a.effective_source || a.source_override || a.source] || sourceName.original
  const editable = can('asset.upload')
  const holder = d.versions.find(v => v.code && v.id !== a.id)
  const op = a.op
  const ops = op?.ops ? Object.keys(op.ops).filter(k => k !== 'format') : []
  const listings = d.used_by.filter((u): u is Extract<UsedBy, { type: 'listing' }> => u.type === 'listing')
  const shopName = (sid: number) => me?.shops.find(s => s.id === sid)?.name || `店铺 ${sid}`

  return (
    <aside className="inspector lib-inspector">
      <div className="inspector-head row between">
        <div className="section-title ellipsis">{assetTitle(a)}</div>
        <button className="close-btn" onClick={onClose} aria-label="关闭"><X /></button>
      </div>
      <div className="inspector-body">
        <div className="inspector-section">
          <a className="lib-preview" href={a.url} target="_blank" rel="noreferrer" title="看原图"><img src={imgUrl(a, 480)} alt="" /></a>
          <div className="inline">
            <Pill kind={src.pill} dot={false}>{src.label}</Pill>
            {a.code ? <span className="chip">{a.code}</span> : <span className="t4 fs-12">未编号</span>}
            {a.archived && <Pill kind="pill-neutral" dot={false}>已归档</Pill>}
            <span className="t3 fs-12 mono">{a.width}×{a.height}</span>
          </div>
        </div>

        <div className="inspector-section">
          <dl className="kv narrow">
            <dt>款</dt><dd><a className="link" onClick={() => onOpenProduct(a.product_id)}>{a.product?.name || `#${a.product_id}`}</a></dd>
            <dt>文件名</dt><dd className="ellipsis" title={a.orig_name}>{a.orig_name}</dd>
            <dt>上传</dt><dd>{fmtDate(a.created_at, true)}</dd>
            {a.ai_note && <><dt>AI 看到</dt><dd className="t2">{a.ai_note}</dd></>}
          </dl>
          {editable && <>
            <label className="form-field"><span className="label">显示名</span>
              <input className="input" value={title} placeholder={a.orig_name} onChange={e => setTitle(e.target.value)}
                onBlur={() => title !== (a.title || '') && patch({ title })} /></label>
            <label className="form-field"><span className="label">备注</span>
              <textarea className="textarea" rows={2} value={note} onChange={e => setNote(e.target.value)}
                onBlur={() => note !== (a.note || '') && patch({ note })} /></label>
          </>}
          <div className="form-field"><span className="label">标签</span>
            {editable ? <TagInput tags={a.tags || []} onChange={tags => patch({ tags })} placeholder="回车添加" />
              : <div className="inline">{(a.tags || []).length ? a.tags.map(t => <span key={t} className="tag">{t}</span>) : <span className="t4 fs-12">没有标签</span>}</div>}
          </div>
        </div>

        {d.versions.length > 1 && (
          <div className="inspector-section">
            <div className="head"><span className="label fs-12 t2" style={{ fontWeight: 600 }}>版本</span><span className="t4 fs-12 mono">{d.versions.length}</span></div>
            <div className="ver-strip">
              {d.versions.map((v, i) => {
                const vs = sourceName[v.effective_source || v.source_override || v.source] || sourceName.original
                return (
                  <button key={v.id} className={'ver' + (v.id === a.id ? ' active' : '')} onClick={() => onSelect(v.id)} title={`${i === 0 ? '原图' : `第 ${i} 次处理`} · ${vs.label}`}>
                    <img src={(v.thumb_url || v.url + '?w=240')} alt="" />
                    {v.code && <span className="ver-code mono"><Star />{v.code}</span>}
                    <span className="ver-n">{i === 0 ? '原图' : `v${i}`}</span>
                  </button>
                )
              })}
            </div>
            {holder && !a.code && editable && a.kind === 'photo' && (
              <button className="btn btn-secondary btn-sm" onClick={() => makeCurrent(holder)}><Star />设为 {holder.code} 的当前用图</button>
            )}
            {a.code && d.versions.length > 1 && <div className="t3 fs-12">这张是 {a.code} 的当前用图，一键出图用它。</div>}
          </div>
        )}

        {op && ops.length > 0 && (
          <div className="inspector-section">
            <div className="head"><span className="label fs-12 t2" style={{ fontWeight: 600 }}>这张是怎么处理出来的</span>{op.elapsed_ms ? <span className="t4 fs-12 mono">{(op.elapsed_ms / 1000).toFixed(1)}s</span> : null}</div>
            <div className="inline">{ops.map(k => <span key={k} className="tag">{opName[k] || k}</span>)}</div>
            {op.ops?.ai?.quick?.length ? <div className="fs-12 t2">快捷需求：{op.ops.ai.quick.map(q => quickOps.find(x => x.key === q)?.label || q).join('、')}</div> : null}
            {op.ops?.ai?.extra ? <div className="fs-12 t2">额外需求：{op.ops.ai.extra}</div> : null}
            {op.diff && <div className="fs-12 t3">和原图差异 <span className="mono">{op.diff.phash_distance}</span>（超过 16 要留意商品外观有没有被改）</div>}
            {(op.warnings || []).map((w, i) => <div key={i} className="status-note fs-12">{w}</div>)}
            {op.models && Object.keys(op.models).length > 0 && <div className="fs-12 t4 mono ellipsis">{Object.values(op.models).join(' · ')}</div>}
          </div>
        )}

        <div className="inspector-section">
          <div className="head"><span className="label fs-12 t2" style={{ fontWeight: 600 }}>被谁用着</span></div>
          {!a.code ? <div className="t4 fs-12">没占拍摄编号，一键出图不会用到它。</div> : listings.length === 0
            ? <div className="t3 fs-12">占着编号 {a.code}，还没有店铺版本的文案引用它。</div>
            : listings.map(l => <div key={l.id} className="row fs-12" style={{ gap: 6 }}><Package className="t3" style={{ width: 14, height: 14 }} />{shopName(l.shop_id)} 的详情页 / 主图</div>)}
        </div>
      </div>
      <div className="inspector-foot lib-actions">
        {editable && <button className="btn btn-secondary btn-sm" onClick={() => onProcess(a.id)}><Wand2 />处理</button>}
        <button className="btn btn-secondary btn-sm" onClick={() => onSimilar(a)}><ScanSearch />找相似</button>
        <a className="btn btn-ghost btn-sm" href={a.url} download><Download />原图</a>
        {editable && <button className="btn btn-ghost btn-sm" onClick={() => patch({ archived: !a.archived }, a.archived ? '已取消归档' : '已归档')}>{a.archived ? <><ArchiveRestore />取消归档</> : <><Archive />归档</>}</button>}
      </div>
    </aside>
  )
}
