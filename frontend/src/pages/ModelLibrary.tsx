import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { Archive, ArchiveRestore, Check, ChevronLeft, ChevronRight, ImagePlus, Pencil, Plus, Search, Sparkles, Star, Trash2, Upload, UserRound } from 'lucide-react'
import { api, qs, type Page } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import type { Job } from '../lib/types'
import { ASPECTS, BODY_PARTS, bodyLabel, expiresAt, modelCover, type Aspect, type BodyPart, type ModelResult, type ShopModel } from '../lib/tryon'
import { Drawer, Empty, Field, Loading, Pill, Topbar } from '../components/ui'
import { ModelImage, ModelJobStatus, useTryOnJob } from '../components/TryOnShared'
import { useLightbox } from '../components/Lightbox'

// 模特可以全局共用（所有店铺能看能用），也可以只给某个店铺；设为全局或从全局收回只有全店角色能做
export function ModelLibrary() {
  const { me, can } = useAuth()
  const toast = useToast()
  const [sp, setSp] = useSearchParams()
  const shops = (me?.shops || []).filter(s => !s.archived)
  // 0 = 全部（全局 + 我能看的所有店铺）
  const [shop, setShop] = useState(() => Number(sp.get('shop_id')) || 0)
  const [q, setQ] = useState('')
  const [archived, setArchived] = useState(false)
  const [page, setPage] = useState(1)
  const [data, setData] = useState<Page<ShopModel> | null>(null)
  const [error, setError] = useState('')
  const [selected, setSelected] = useState<ShopModel | null>(null)
  const [editing, setEditing] = useState<ShopModel | 'new' | null>(null)
  const allowed = can('model.view') && me?.role.scope !== 'shares'
  const editable = can('model.manage')
  const selectedId = selected?.id
  const load = useCallback(async () => {
    if (!allowed) return
    setError('')
    try {
      const r = await api<Page<ShopModel>>('/api/models' + qs({ shop_id: shop || undefined, q, archived: archived ? 1 : 0, page, page_size: 24 }))
      setData(r)
    } catch (e) { setError(errText(e)); setData(null) }
  }, [allowed, shop, q, archived, page])
  useEffect(() => { const timer = setTimeout(() => { setData(null); load() }, 180); return () => clearTimeout(timer) }, [load])
  useEffect(() => {
    const id = Number(sp.get('model'))
    if (!id || !allowed) return
    let canceled = false
    api<{ model: ShopModel }>(`/api/models/${id}`).then(r => {
      if (canceled) return
      setSelected(r.model)
    }).catch(e => { if (!canceled) toast(errText(e), 'error') })
    return () => { canceled = true }
  }, [sp, allowed, toast])
  const refreshSelected = useCallback(async () => {
    load()
    if (!selectedId) return
    try { const r = await api<{ model: ShopModel }>(`/api/models/${selectedId}`); setSelected(r.model) }
    catch (e) { toast(errText(e), 'error'); setSelected(null) }
  }, [load, selectedId, toast])
  const shopParam = (id: number): Record<string, string> => id ? { shop_id: String(id) } : {}
  const open = (m: ShopModel) => { setSelected(m); setSp({ ...shopParam(shop), model: String(m.id) }, { replace: true }) }
  const close = () => { setSelected(null); setSp(shopParam(shop), { replace: true }) }
  const switchShop = (id: number) => { setShop(id); setSelected(null); setPage(1); setSp(shopParam(id), { replace: true }) }
  const scopeName = (m: ShopModel) => m.shop_id === null ? '全局' : shops.find(s => s.id === m.shop_id)?.name || `店铺 ${m.shop_id}`
  if (!allowed) return <><Topbar title="模特库" /><Empty title="没有模特库访问权限" desc="需要店铺模特查看权限及对应店铺的访问权。" /></>
  return <>
    <Topbar title="模特库" sub="固定人物与局部参考；全局模特所有店铺共用，也可以只给某个店铺">
      {editable && <button className="btn btn-primary" onClick={() => setEditing('new')}><Plus />创建模特</button>}
    </Topbar>
    <div className="scroll"><main className="content model-library">
      <div className="toolbar model-toolbar">
        <select aria-label="模特范围" className="input" value={shop} onChange={e => switchShop(Number(e.target.value))}><option value={0}>全部（全局 + 各店铺）</option>{shops.map(s => <option key={s.id} value={s.id}>{s.name} 能用的（含全局）</option>)}</select>
        <label className="field"><Search /><input placeholder="搜索模特名称、风格" value={q} onChange={e => { setQ(e.target.value); setPage(1) }} /></label>
        <div className="segmented"><button className={!archived ? 'active' : ''} onClick={() => { setArchived(false); setPage(1) }}>使用中</button><button className={archived ? 'active' : ''} onClick={() => { setArchived(true); setPage(1) }}>已归档</button></div>
        <span className="grow" /><span className="help mono">{data ? `${data.total} 位模特` : ''}</span>
      </div>
      {error ? <Empty title="模特库加载失败" desc={error}><button className="btn btn-secondary" onClick={load}>重试</button></Empty> : !data ? <Loading /> : !data.items.length ? <section className="panel"><Empty title={q ? '没有找到模特' : archived ? '暂无归档模特' : '为店铺创建第一位模特'} desc={q ? '换个关键词试试。' : '可以上传人物参考图，也可以用描述生成虚拟模特；手模、耳模和颈模都能独立建档。'}>{editable && !archived && <button className="btn btn-secondary" onClick={() => setEditing('new')}><UserRound />创建模特档案</button>}</Empty></section> : <div className="model-grid">
        {data.items.map(m => <button key={m.id} className="model-card" onClick={() => open(m)}>
          <div className="model-card-photo"><ModelImage src={modelCover(m)} alt={m.name} /><span className="model-card-count mono">{m.references?.length || 0} 张参考</span></div>
          <div className="model-card-info"><div className="row between"><b className="ellipsis">{m.name}</b><span className="inline">{m.archived && <Pill kind="pill-neutral" dot={false}>已归档</Pill>}<Pill kind={m.shop_id === null ? 'pill-teal' : 'pill-neutral'} dot={false}>{scopeName(m)}</Pill></span></div><p className="model-description">{m.description || '尚未填写外观与风格描述'}</p><div className="inline">{[...new Set((m.references || []).map(r => r.body_part))].slice(0, 5).map(p => <span className="tag" key={p}>{bodyLabel(p)}</span>)}{!m.references?.length && <span className="help">添加参考图后可用于试穿</span>}</div></div>
        </button>)}
      </div>}
      {data && data.total > 24 && <div className="pager"><button className="btn btn-secondary btn-sm" disabled={page === 1} onClick={() => setPage(p => p - 1)}><ChevronLeft />上一页</button><span className="mono">{page} / {Math.ceil(data.total / 24)}</span><button className="btn btn-secondary btn-sm" disabled={page * 24 >= data.total} onClick={() => setPage(p => p + 1)}>下一页<ChevronRight /></button></div>}
    </main></div>
    {selected && <ModelDetail key={selected.id} model={selected} onClose={close} onChange={refreshSelected} onEdit={() => setEditing(selected)} onDeleted={() => { close(); load() }} />}
    {editing && <ModelForm model={editing === 'new' ? undefined : editing} shopId={shop} onClose={() => setEditing(null)} onSaved={m => { setEditing(null); setSelected(m); setSp({ ...shopParam(shop), model: String(m.id) }, { replace: true }); load() }} />}
  </>
}

function ModelForm({ model, shopId, onClose, onSaved }: { model?: ShopModel; shopId: number; onClose: () => void; onSaved: (m: ShopModel) => void }) {
  const toast = useToast()
  const { me } = useAuth()
  const shops = (me?.shops || []).filter(s => !s.archived)
  const allShops = me?.role.scope === 'all'
  // 范围：0 = 全局；新建默认用当前筛的店铺，全店角色在「全部」下默认全局
  const [scope, setScope] = useState(model ? model.shop_id || 0 : shopId || (allShops ? 0 : shops[0]?.id || 0))
  const scopeLocked = !allShops && (model?.shop_id === null)
  const [name, setName] = useState(model?.name || '')
  const [description, setDescription] = useState(model?.description || '')
  const [busy, setBusy] = useState(false)
  const save = async () => {
    setBusy(true)
    try { const r = await api<{ model: ShopModel }>(model ? `/api/models/${model.id}` : '/api/models', { method: model ? 'PATCH' : 'POST', body: { name: name.trim(), description: description.trim(), ...(!model || (model.shop_id || 0) !== scope ? { shop_id: scope || null } : {}) } }); onSaved(r.model); toast(model ? '档案已更新' : '已创建档案，接着添加固定参考图', 'ok') }
    catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  return <Drawer title={model ? '编辑模特档案' : '创建店铺模特'} width={440} onClose={onClose} foot={<><button className="btn btn-secondary" onClick={onClose}>取消</button><button className="btn btn-primary" onClick={save} disabled={busy || !name.trim()}>{busy ? '保存中…' : model ? '保存档案' : '创建并添加参考图'}</button></>}>
    <Field label="谁能用" hint={scopeLocked ? '全局模特只有全店角色能收回到某个店铺' : allShops ? '全局：所有店铺都能看能用' : '设为全局要全店角色来做'}>
      <select className="input" value={scope} disabled={scopeLocked} onChange={e => setScope(Number(e.target.value))}>
        {(allShops || scope === 0) && <option value={0}>全局（所有店铺）</option>}
        {shops.map(s => <option key={s.id} value={s.id}>只给 {s.name}</option>)}
      </select>
    </Field>
    <Field label="模特名称" req><input className="input" autoFocus maxLength={80} placeholder="如：小林 · 通勤、白皙手模" value={name} onChange={e => setName(e.target.value)} /></Field>
    <Field label="外观与风格" hint="描述会用于 AI 创建与试穿；稳定的外观特征配合固定参考图，可帮助保持人物一致。"><textarea className="textarea" rows={7} maxLength={4000} value={description} onChange={e => setDescription(e.target.value)} placeholder="例如：成年东亚女性，黑色齐肩发，自然肤色，清冷简洁风格。也可以只描述修长手指、短圆甲等局部特征。" /></Field>
    {!model && <div className="model-note">创建后可上传照片，或按描述生成虚拟模特。生成图经确认后才成为固定参考。</div>}
  </Drawer>
}

function ModelDetail({ model, onClose, onChange, onEdit, onDeleted }: { model: ShopModel; onClose: () => void; onChange: () => void; onEdit: () => void; onDeleted: () => void }) {
  const { can } = useAuth()
  const lightbox = useLightbox()
  const refItems = (model.references || []).map(r => ({ src: r.url, title: `${model.name} · ${bodyLabel(r.body_part)}`, download: '' }))
  const toast = useToast()
  const editable = can('model.manage')
  const ai = editable && can('asset.process') && can('asset.upload')
  const [part, setPart] = useState<BodyPart>('upper_body')
  const [aspect, setAspect] = useState<Aspect>('2:3')
  const [base, setBase] = useState(model.cover_reference_id || model.references?.[0]?.id || 0)
  const [prompt, setPrompt] = useState('')
  const [busy, setBusy] = useState(false)
  const [candidates, setCandidates] = useState<ModelResult[]>([])
  const [candidateError, setCandidateError] = useState('')
  const [cover, setCover] = useState(!model.references?.length)
  useEffect(() => {
    if (!model.references?.some(r => r.id === base)) setBase(model.cover_reference_id || model.references?.[0]?.id || 0)
    // 新档案首次上传后默认沿用基准，后续局部图继续参照同一人物。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [model.references, model.cover_reference_id])
  const input = useRef<HTMLInputElement>(null)
  const task = useTryOnJob(ai)
  const handled = useRef(0)
  const loadCandidates = useCallback(async () => {
    if (!ai) return
    try { const r = await api<Page<ModelResult>>('/api/workbench/items?kind=result&operation=model_candidate'); setCandidates(r.items.filter(i => i.op?.model_id === model.id)); setCandidateError('') }
    catch (e) { setCandidateError(errText(e)) }
  }, [ai, model.id])
  useEffect(() => { loadCandidates() }, [loadCandidates])
  useEffect(() => {
    if (!task.job || task.running || handled.current === task.job.id) return
    handled.current = task.job.id
    if (task.job.status === 'failed') toast(task.job.message || '生成失败，可调整后重试', 'error')
    loadCandidates()
  }, [task.job, task.running, loadCandidates, toast])
  const upload = async (files: FileList | null) => {
    if (!files?.length) return
    const f = files[0]
    if (f.size > 25 * 1024 * 1024) return toast('单张图片不能超过 25 MB', 'error')
    const form = new FormData(); form.append('files', f); form.append('body_part', part)
    setBusy(true)
    try { await api(`/api/models/${model.id}/references`, { form }); toast('固定参考图已保存', 'ok'); onChange() }
    catch (e) { toast(errText(e), 'error') } finally { setBusy(false); if (input.current) input.current.value = '' }
  }
  const generate = async () => {
    setBusy(true)
    try { const r = await api<{ job: Job }>(`/api/models/${model.id}/generate`, { body: { body_part: part, aspect_ratio: aspect, prompt, ...(base ? { base_reference_id: base } : {}) } }); task.start(r.job) }
    catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const confirmCandidate = async (item: ModelResult) => {
    setBusy(true)
    try { await api(`/api/models/${model.id}/references/confirm`, { body: { item_id: item.id, set_cover: cover } }); toast('已保存为固定参考图', 'ok'); onChange(); loadCandidates() }
    catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const patch = async (body: Record<string, unknown>) => {
    setBusy(true)
    try { await api(`/api/models/${model.id}`, { method: 'PATCH', body }); onChange() }
    catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const removeModel = async () => {
    if (!confirm(`永久删除模特「${model.name}」？
参考图会一起删掉，不能恢复；用这位模特已经生成的商品图会保留。`)) return
    setBusy(true)
    try { await api(`/api/models/${model.id}`, { method: 'DELETE' }); toast('模特已删除', 'ok'); onDeleted() }
    catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const removeReference = async (id: number) => {
    if (!confirm('删除这张固定参考图？已有生成结果会保留，之后不能再用这张参考图生成。')) return
    setBusy(true)
    try {
      await api(`/api/models/${model.id}/references/${id}`, { method: 'DELETE' })
      toast('固定参考图已删除', 'ok'); onChange(); loadCandidates()
    } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  return <Drawer title={<span className="inline"><UserRound size={16} />{model.name}</span>} width={620} onClose={onClose} foot={<>
    {editable && <button className="btn btn-ghost" disabled={busy || task.running} onClick={() => patch({ archived: !model.archived })}>{model.archived ? <ArchiveRestore /> : <Archive />}{model.archived ? '恢复使用' : '归档模特'}</button>}
    {editable && model.archived && <button className="btn btn-danger" disabled={busy} onClick={removeModel}><Trash2 />永久删除</button>}
    <span className="grow" />{!model.archived && model.references?.length > 0 && can('asset.process') && can('asset.upload') && <Link className="btn btn-secondary" to={`/tryon?model=${model.id}${model.shop_id ? `&shop_id=${model.shop_id}` : ''}`}><Sparkles />用这位模特试穿</Link>}
  </>}>
    <div className="row between"><span className="help">{model.shop_id === null ? '全局模特 · 所有店铺可用' : '店铺专属'} · 固定参考长期保留 · {model.references?.length || 0} 张</span>{editable && <button className="btn btn-ghost btn-sm" onClick={onEdit}><Pencil />编辑档案</button>}</div>
    <p className="model-detail-description">{model.description || '尚未填写外观与风格描述'}</p>
    {model.archived && <div className="model-note">已归档，恢复使用后可以继续添加参考图和生成穿戴照；不再需要可以在下方永久删除。</div>}
    <div className="model-ref-grid">{(model.references || []).map((r, ri) => <div className="model-ref" key={r.id}>
      <button type="button" className="model-ref-photo" title="看大图" onClick={() => lightbox(refItems, ri)}><ModelImage src={r.thumb_url || r.url} alt={`${model.name} ${bodyLabel(r.body_part)}`} /></button>
      <div className="row between"><span>{bodyLabel(r.body_part)}</span>{r.id === model.cover_reference_id ? <span className="inline help"><Star size={12} />封面</span> : editable && !model.archived && <button className="btn btn-ghost btn-sm" disabled={busy} onClick={() => patch({ cover_reference_id: r.id })}>设封面</button>}</div>
      {editable && <button className="btn btn-ghost btn-sm danger-text model-ref-delete" disabled={busy || task.running} onClick={() => removeReference(r.id)}><Trash2 size={14} />删除参考图</button>}
    </div>)}</div>
    {!model.references?.length && <div className="model-note">先为这位模特添加一张人物或局部参考。确认后的参考图会反复用于商品穿戴。</div>}
    {editable && !model.archived && <section className="model-reference-form stack">
      <div className="section-title inline"><ImagePlus size={16} />添加固定参考</div>
      <div className="form-row"><Field label="参考部位"><select className="input" value={part} onChange={e => { setPart(e.target.value as BodyPart); if (!base && model.references?.length && e.target.value !== 'full_body') setBase(model.cover_reference_id || model.references[0].id) }}>{Object.entries(BODY_PARTS).map(([k, v]) => <option value={k} key={k}>{v}</option>)}</select></Field><Field label="画幅"><select className="input" value={aspect} onChange={e => setAspect(e.target.value as Aspect)}>{Object.entries(ASPECTS).map(([k, v]) => <option key={k} value={k}>{v}</option>)}</select></Field></div>
      <button className="btn btn-secondary" disabled={busy || task.running} onClick={() => input.current?.click()}><Upload />上传这类参考图</button><input ref={input} type="file" hidden accept="image/jpeg,image/png,image/webp" onChange={e => upload(e.target.files)} />
      {ai && <>
        {model.references?.length > 0 && <Field label="生成时参照" hint="选已有基准有助于保留同一人物的特征。"><select className="input" value={base} onChange={e => setBase(Number(e.target.value))}>{part === 'full_body' && <option value={0}>不使用图片，按描述创建</option>}{model.references.map(r => <option key={r.id} value={r.id}>#{r.id} · {bodyLabel(r.body_part)}</option>)}</select></Field>}
        <Field label="补充拍摄要求"><textarea className="textarea" rows={3} maxLength={4000} value={prompt} onChange={e => setPrompt(e.target.value)} placeholder="如：耳垂侧面特写，头发别到耳后，不戴耳饰；或手指自然展开、短圆甲。" /></Field>
        <button className="btn btn-primary" disabled={busy || task.running || (!base && !model.description.trim() && !prompt.trim())} onClick={generate}><Sparkles />{task.running ? '生成中…' : base ? '生成同模特参考' : '按描述生成模特'}</button>
        <span className="help">每次生成 1 张，使用本人 AI 生图额度；确认后才固定入库。</span>
        <ModelJobStatus job={task.job} running={task.running} />
      </>}
    </section>}
    {ai && (candidateError || candidates.length > 0) && <section className="stack">
      <div className="row between"><span className="section-title">生成候选</span><span className="help">暂存 7 天</span></div>
      {candidateError && <div className="form-err">{candidateError}</div>}
      {!model.archived && <label className="inline help"><input className="check" type="checkbox" checked={cover} onChange={e => setCover(e.target.checked)} />确认时设为档案封面</label>}
      <div className="model-candidates">{candidates.map((i, ci) => <article className="model-candidate" key={i.id}>
        <button type="button" title="看大图" onClick={() => lightbox(candidates.map(c => ({ src: c.url, title: `候选 · ${bodyLabel(c.op?.body_part)}`, meta: `${expiresAt(c.created_at)} 到期` })), ci)}><img src={i.thumb_url || i.url} alt="生成的模特候选" /></button>
        <div className="stack"><div className="row between"><b>{bodyLabel(i.op?.body_part)}</b><span className="help">{expiresAt(i.created_at)} 到期</span></div><p className="help">{i.op?.prompt || '按模特档案生成'}</p>
          {i.op?.confirmed_reference_id ? <span className="inline t3"><Check size={14} />已保存为固定参考</span> : !model.archived && <button className="btn btn-secondary" disabled={busy || task.running} onClick={() => confirmCandidate(i)}><Check />确认保存到模特库</button>}
        </div>
      </article>)}</div>
    </section>}
  </Drawer>
}
