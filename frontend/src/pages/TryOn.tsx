import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { ArrowLeftRight, Download, Images, Plus, RefreshCw, Save, ScanSearch, Sparkles, Trash2, Upload, UserRound, X } from 'lucide-react'
import { api, qs, type Page } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import type { Asset, Job, WbItem } from '../lib/types'
import { ASPECTS, BODY_PARTS, FRAMINGS, bodyLabel, expiresAt, modelCover, resultIds, sourceImage, sourceInput, sourceLabel, type Analysis, type Aspect, type BodyPart, type Framing, type ModelResult, type ShopModel, type TryOnSource } from '../lib/tryon'
import { Empty, Field, Loading, Topbar } from '../components/ui'
import { ModelImage, ModelJobStatus, TryOnAssetPicker, useTryOnJob } from '../components/TryOnShared'
import { SaveDialog } from './Workbench'

export function TryOn() {
  const { me, can } = useAuth()
  const toast = useToast()
  const [sp, setSp] = useSearchParams()
  const shops = (me?.shops || []).filter(s => !s.archived)
  const allowed = can('model.view') && can('asset.process') && can('asset.upload') && me?.role.scope !== 'shares'
  const [shop, setShop] = useState(() => Number(sp.get('shop_id')) || shops[0]?.id || 0)
  const [models, setModels] = useState<ShopModel[] | null>(null)
  const [modelError, setModelError] = useState('')
  const [modelId, setModelId] = useState(() => Number(sp.get('model')) || 0)
  const [referenceId, setReferenceId] = useState(0)
  const [srcs, setSrcs] = useState<TryOnSource[]>([])
  const [picking, setPicking] = useState(false)
  const [results, setResults] = useState<ModelResult[] | null>(null)
  const [resultError, setResultError] = useState('')
  const [selected, setSelected] = useState<number | null>(null)
  const [saving, setSaving] = useState<ModelResult | null>(null)
  const [part, setPart] = useState<BodyPart>('ear')
  const [framing, setFraming] = useState<Framing>('closeup')
  const [aspect, setAspect] = useState<Aspect>('2:3')
  const [productType, setProductType] = useState('')
  const [prompt, setPrompt] = useState('')
  const [hint, setHint] = useState('')
  const [analysis, setAnalysis] = useState<Analysis | null>(null)
  const [busy, setBusy] = useState(false)
  const [uploading, setUploading] = useState(false)
  const [loadingSources, setLoadingSources] = useState(false)
  const [compare, setCompare] = useState(true)
  const [resultOriginal, setResultOriginal] = useState<string>('')
  const [over, setOver] = useState(false)
  const file = useRef<HTMLInputElement>(null)
  const handled = useRef(0)
  const queryLoaded = useRef(false)
  const sourceVersion = useRef(0)
  const analyzedVersion = useRef(-1)
  const task = useTryOnJob(allowed)
  const model = models?.find(m => m.id === modelId)
  const reference = model?.references?.find(r => r.id === referenceId)
  const current = results?.find(r => r.id === selected)
  const controlsLocked = busy || uploading || task.running || loadingSources
  useEffect(() => {
    setResultOriginal('')
    if (!current) return
    let canceled = false
    const from = current.asset_id ? api<{ asset: Asset }>(`/api/assets/${current.asset_id}`).then(r => r.asset.url)
      : current.input_id ? api<{ item: WbItem }>(`/api/workbench/items/${current.input_id}`).then(r => r.item.url) : Promise.resolve('')
    from.then(url => { if (!canceled) setResultOriginal(url) }).catch(() => {})
    return () => { canceled = true }
  }, [current?.id, current?.asset_id, current?.input_id])

  const clearAnalysis = useCallback(() => { sourceVersion.current++; setAnalysis(null); setProductType(''); setHint('') }, [])
  const loadResults = useCallback(async () => {
    if (!allowed) return
    try { const r = await api<Page<ModelResult>>('/api/workbench/items?kind=result&operation=tryon'); setResults(r.items); setResultError('') }
    catch (e) { setResultError(errText(e)); setResults([]) }
  }, [allowed])
  useEffect(() => { loadResults() }, [loadResults])
  useEffect(() => {
    if (!allowed || !shop) { setModels([]); return }
    let canceled = false
    setModels(null); setModelError('')
    api<Page<ShopModel>>('/api/models' + qs({ shop_id: shop, page_size: 200 })).then(r => {
      if (canceled) return
      setModels(r.items)
      setModelId(old => r.items.some(m => m.id === old) ? old : r.items.find(m => m.references?.length)?.id || 0)
    }).catch(e => { if (!canceled) { setModels([]); setModelError(errText(e)) } })
    return () => { canceled = true }
  }, [allowed, shop])
  useEffect(() => {
    if (!model) { setReferenceId(0); return }
    setReferenceId(model.references?.find(r => r.body_part === part)?.id || model.cover_reference_id || model.references?.[0]?.id || 0)
  }, [model, part])
  useEffect(() => {
    if (!allowed || queryLoaded.current) return
    queryLoaded.current = true
    const queryModel = Number(sp.get('model'))
    if (queryModel && !sp.get('shop_id')) api<{ model: ShopModel }>(`/api/models/${queryModel}`).then(r => { setShop(r.model.shop_id); setModelId(r.model.id) }).catch(e => toast(errText(e), 'error'))
    const ids = [...new Set((sp.get('assets') || '').split(',').map(Number).filter(n => Number.isSafeInteger(n) && n > 0))]
    if (ids.length > 2) toast('一次最多使用 2 张同款商品图，请重新选择', 'error')
    else if (ids.length) {
      setLoadingSources(true)
      Promise.all(ids.map(id => api<{ asset: Asset }>(`/api/assets/${id}`).then(r => r.asset))).then(list => {
        if (list.some(a => a.kind !== 'photo' || a.product_id !== list[0].product_id)) { toast('请选择同一商品的素材照片', 'error'); return }
        clearAnalysis(); setSrcs(list.map(asset => ({ key: 'a' + asset.id, asset })))
      }).catch(e => toast(errText(e), 'error')).finally(() => setLoadingSources(false))
    }
    setSp(new URLSearchParams(), { replace: true })
  }, [allowed, sp, setSp, toast, clearAnalysis])
  useEffect(() => {
    if (!task.job || task.running || handled.current === task.job.id) return
    handled.current = task.job.id
    if (task.job.status === 'failed') { toast(task.job.message || '生成失败，请调整后重试', 'error'); return }
    if (task.job.kind === 'assets.tryon_analyze') {
      if (analyzedVersion.current !== -1 && analyzedVersion.current !== sourceVersion.current) return
      const r = task.job.result as Analysis | null
      if (r && r.product_type && r.body_part in BODY_PARTS && r.framing in FRAMINGS) {
        setAnalysis(r); setProductType(r.product_type); setPart(r.body_part); setFraming(r.framing)
        toast('已识别商品，可修改类型、部位和拍摄要求', 'ok')
      } else toast('识别未返回完整建议，可以直接手动填写后生成', 'error')
    } else if (task.job.kind === 'assets.tryon') {
      loadResults().then(() => { const ids = resultIds(task.job?.result); if (ids[0]) setSelected(ids[0]) })
      toast('穿戴照已生成，请对照商品检查细节', 'ok')
    }
  }, [task.job, task.running, toast, loadResults])
  const changeSources = (list: Asset[]) => {
    clearAnalysis(); setPrompt(''); setSelected(null)
    setSrcs(list.map(asset => ({ key: 'a' + asset.id, asset }))); setPicking(false)
  }
  const upload = useCallback(async (files: File[]) => {
    if (controlsLocked) return
    if (!files.length || files.length > 2) { toast('请选择 1–2 张同款商品图', 'error'); return }
    if (files.some(f => !/^image\/(jpeg|png|webp)$/.test(f.type) || f.size > 25 * 1024 * 1024)) { toast('支持 jpg、png、webp，单张不超过 25 MB', 'error'); return }
    const form = new FormData(); files.forEach(f => form.append('files', f, f.name || '商品图.png'))
    setUploading(true)
    try { const r = await api<{ items: WbItem[] }>('/api/workbench/inputs', { form }); clearAnalysis(); setPrompt(''); setSelected(null); setSrcs(r.items.map(item => ({ key: 'u' + item.id, item }))) }
    catch (e) { toast(errText(e), 'error') } finally { setUploading(false); if (file.current) file.current.value = '' }
  }, [controlsLocked, toast, clearAnalysis])
  useEffect(() => {
    if (!allowed) return
    const paste = (e: ClipboardEvent) => {
      const target = e.target as HTMLElement
      if (target.closest('input,textarea,[contenteditable="true"]')) return
      const list = Array.from(e.clipboardData?.files || []).filter(f => f.type.startsWith('image/'))
      if (list.length) { e.preventDefault(); upload(list) }
    }
    window.addEventListener('paste', paste); return () => window.removeEventListener('paste', paste)
  }, [upload, allowed])
  const analyze = async () => {
    setBusy(true)
    try { const r = await api<{ job: Job }>('/api/tryon/analyze', { body: { inputs: srcs.map(sourceInput), hint } }); analyzedVersion.current = sourceVersion.current; task.start(r.job) }
    catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const generate = async () => {
    setBusy(true)
    try { const r = await api<{ job: Job }>('/api/tryon/generate', { body: { inputs: srcs.map(sourceInput), model_id: modelId, reference_id: referenceId, body_part: part, framing, product_type: productType, prompt, aspect_ratio: aspect } }); task.start(r.job) }
    catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const remove = async (item: ModelResult) => {
    if (!confirm('删除这张暂存结果？已保存到素材库的副本会保留。')) return
    try { await api(`/api/workbench/items/${item.id}`, { method: 'DELETE' }); if (selected === item.id) setSelected(null); loadResults() }
    catch (e) { toast(errText(e), 'error') }
  }
  const choosePart = (value: BodyPart) => {
    setPart(value)
    if (model) setReferenceId(model.references?.find(r => r.body_part === value)?.id || model.cover_reference_id || model.references?.[0]?.id || 0)
  }
  const canGenerate = srcs.length > 0 && !!reference && !!(productType.trim() || prompt.trim()) && !controlsLocked
  if (!allowed) return <><Topbar title="AI 试穿试戴" /><Empty title="暂无试穿试戴权限" desc="需要图片上传、AI 图片处理、模特查看权限及店铺访问权。" /></>
  return <>
    <Topbar title="AI 试穿试戴" sub="选择商品，使用店铺固定模特拍摄穿戴照"><Link className="btn btn-secondary" to="/models"><UserRound />模特库</Link></Topbar>
    <div className="tryon-layout">
      <aside className="tryon-inputs">
        <div className="tryon-section">
          <div className="row between"><b className="section-title">商品图片</b><span className="help mono">{srcs.length} / 2</span></div>
          <div className="help">第一张为主图，可加一张同款细节图。</div>
          {loadingSources ? <Loading /> : <div className="tryon-source-list">{srcs.map((s, i) => <div className="tryon-source" key={s.key}><img src={sourceImage(s)} alt={sourceLabel(s)} /><div><b>{i === 0 ? '主商品图' : '辅助细节图'}</b><span className="help ellipsis">{sourceLabel(s)}</span></div><button className="btn btn-ghost btn-sm btn-icon" disabled={controlsLocked} title="移除图片" onClick={() => { clearAnalysis(); setPrompt(''); setSelected(null); setSrcs(v => v.filter(x => x.key !== s.key)) }}><X /></button></div>)}</div>}
          {srcs.length === 2 && <button className="btn btn-ghost btn-sm" disabled={controlsLocked} onClick={() => { clearAnalysis(); setPrompt(''); setSelected(null); setSrcs(v => [v[1], v[0]]) }}><ArrowLeftRight />交换主图与细节图</button>}
          <div className="model-button-pair"><button className="btn btn-secondary" disabled={controlsLocked} onClick={() => setPicking(true)}><Images />选商品图</button><button className="btn btn-secondary" disabled={controlsLocked} onClick={() => file.current?.click()}><Upload />上传</button></div>
          <div className={'tryon-drop' + (over ? ' over' : '')} onDragOver={e => { e.preventDefault(); if (!controlsLocked) setOver(true) }} onDragLeave={() => setOver(false)} onDrop={e => { e.preventDefault(); setOver(false); upload(Array.from(e.dataTransfer.files)) }}><span>{uploading ? '上传中…' : '也可拖入或粘贴商品图'}</span></div>
          <input hidden ref={file} type="file" accept="image/jpeg,image/png,image/webp" multiple onChange={e => upload(Array.from(e.target.files || []))} />
        </div>
        <div className="tryon-section">
          <div className="row between"><b className="section-title">店铺模特</b><Link className="link fs-12" to={`/models?shop_id=${shop}`}><Plus size={12} />管理</Link></div>
          <select className="input" aria-label="选择店铺" value={shop} disabled={controlsLocked} onChange={e => { setShop(Number(e.target.value)); setModelId(0); setReferenceId(0) }}>{!shops.length && <option value={0}>暂无可用店铺</option>}{shops.map(s => <option key={s.id} value={s.id}>{s.name}</option>)}</select>
          {modelError ? <div className="form-err">{modelError}</div> : !models ? <Loading /> : !models.length ? <div className="model-note">这家店还没有模特。<Link className="link" to={`/models?shop_id=${shop}`}>去创建</Link></div> : <>
            <select className="input" aria-label="选择固定模特" disabled={controlsLocked} value={modelId} onChange={e => { setModelId(Number(e.target.value)); setReferenceId(0) }}><option value={0}>选择模特</option>{models.map(m => <option key={m.id} value={m.id}>{m.name}{!m.references?.length ? '（待添加参考）' : ''}</option>)}</select>
            {model && <><div className="tryon-model"><div className="tryon-model-avatar"><ModelImage src={modelCover(model)} alt={model.name} /></div><div><b>{model.name}</b><span className="help model-description">{model.description || '固定店铺模特'}</span></div></div>
              {model.references?.length ? <><span className="label">本次人物 / 部位参考</span><div className="tryon-reference-list">{model.references.map(r => <button key={r.id} title={`${bodyLabel(r.body_part)} #${r.id}`} disabled={controlsLocked} className={'tryon-reference' + (r.id === referenceId ? ' active' : '')} onClick={() => setReferenceId(r.id)}><img src={r.thumb_url || r.url} alt={bodyLabel(r.body_part)} /><span>{bodyLabel(r.body_part)}</span></button>)}</div><div className="help">优先选匹配部位的参考，也可用人物基准图生成特写。</div></> : <Link className="link" to={`/models?shop_id=${shop}&model=${model.id}`}>为这位模特添加参考图</Link>}
            </>}
          </>}
        </div>
      </aside>
      <main className="tryon-stage">
        <div className="tryon-stage-head"><span className="inline"><span className="tag">{current ? 'AI 生成' : '拍摄预览'}</span>{current && <span className="help">{current.op?.model_name} · {bodyLabel(current.op?.body_part)}</span>}</span><span className="grow" />{current && resultOriginal && <label className="inline help"><input type="checkbox" className="check" checked={compare} onChange={e => setCompare(e.target.checked)} />对照商品</label>}</div>
        <div className="tryon-preview">
          {current ? <div className={'tryon-comparison' + (compare && resultOriginal ? ' with-original' : '')}>{compare && resultOriginal && <figure><img src={resultOriginal} alt="这次生成使用的商品原图" /><figcaption>生成时的商品原图</figcaption></figure>}<figure><img src={current.url + '?w=960'} alt="AI 模特穿戴结果" /><figcaption>穿戴效果 · 请核对商品细节与人体结构</figcaption></figure></div> : <div className="tryon-preview-empty"><div className="tryon-preview-symbol"><UserRound /><Sparkles /></div><h2>让商品穿戴在你的模特身上</h2><p>选择商品图片和模特，指定部位与构图。<br />耳饰、项链、手链可直接生成局部特写。</p>{srcs[0] && <div className="tryon-ready-images"><img src={sourceImage(srcs[0])} alt="已选商品" /><Plus />{reference ? <img src={reference.thumb_url || reference.url} alt="已选模特参考" /> : <span><UserRound /></span>}</div>}</div>}
        </div>
        {task.job && <ModelJobStatus job={task.job} running={task.running} />}
        {current && <div className="tryon-result-actions"><span className="help">{current.saved_asset_id ? '已保存到素材库' : `暂存至 ${expiresAt(current.created_at)}`} · <span className="mono">{current.width}×{current.height}</span></span><span className="grow" /><button className="btn btn-ghost btn-sm" onClick={() => remove(current)}><Trash2 />删除暂存</button>{can('export.run') && <a className="btn btn-secondary btn-sm" href={`${current.url}?download=1`} download><Download />下载</a>}<button className="btn btn-secondary btn-sm" disabled={!!current.saved_asset_id} onClick={() => setSaving(current)}><Save />{current.saved_asset_id ? '已保存' : '保存到素材库'}</button></div>}
        <section className="tryon-history"><div className="row between"><span className="section-title">最近结果 <span className="help mono">{results?.length || 0}</span></span><button className="btn btn-ghost btn-sm" onClick={loadResults}><RefreshCw />刷新</button></div>{resultError ? <div className="form-err">{resultError}</div> : !results ? <Loading /> : !results.length ? <div className="help">生成后会出现在这里。结果暂存 7 天，保存到素材库后长期保留。</div> : <div className="tryon-history-strip">{results.map(r => <button key={r.id} className={'tryon-history-item' + (selected === r.id ? ' active' : '')} onClick={() => setSelected(r.id)}><img src={r.thumb_url || r.url} alt={`${r.op?.model_name || '模特'} ${bodyLabel(r.op?.body_part)}`} loading="lazy" /><span>{r.op?.model_name || '穿戴照'}</span><span className="help">{bodyLabel(r.op?.body_part)}{r.saved_asset_id ? ' · 已保存' : ''}</span></button>)}</div>}</section>
      </main>
      <aside className="tryon-settings">
        <div className="tryon-section">
          <div className="row between"><b className="section-title">拍摄设置</b><span className="help">可直接手动填写</span></div>
          <Field label="识别时的提示（可选）"><input className="input" disabled={controlsLocked} value={hint} maxLength={4000} onChange={e => setHint(e.target.value)} placeholder="如：这是耳钉，注意背扣结构" /></Field>
          <button className="btn btn-secondary" disabled={!srcs.length || controlsLocked} onClick={analyze}><ScanSearch />{task.running && task.job?.kind === 'assets.tryon_analyze' ? '正在识别…' : 'AI 识别商品与佩戴位置'}</button>
          {analysis && <div className="model-note"><b>识别建议</b><p>{analysis.description}</p>{analysis.suggested_prompt && <button className="btn btn-ghost btn-sm" disabled={controlsLocked} onClick={() => setPrompt(p => [p, analysis.suggested_prompt].filter(Boolean).join('\n'))}>将拍摄建议填入要求</button>}</div>}
          <Field label="商品类型"><input className="input" disabled={controlsLocked} maxLength={200} value={productType} onChange={e => setProductType(e.target.value)} placeholder="如：珍珠耳钉、项链、连衣裙" /></Field>
          <Field label="穿戴部位"><select className="input" disabled={controlsLocked} value={part} onChange={e => choosePart(e.target.value as BodyPart)}>{Object.entries(BODY_PARTS).map(([k, v]) => <option key={k} value={k}>{v}</option>)}</select></Field>
          <div className="form-row"><Field label="取景"><select className="input" disabled={controlsLocked} value={framing} onChange={e => setFraming(e.target.value as Framing)}>{Object.entries(FRAMINGS).map(([k, v]) => <option value={k} key={k}>{v}</option>)}</select></Field><Field label="画幅"><select className="input" disabled={controlsLocked} value={aspect} onChange={e => setAspect(e.target.value as Aspect)}>{Object.entries(ASPECTS).map(([k, v]) => <option value={k} key={k}>{v}</option>)}</select></Field></div>
          <Field label="补充拍摄要求" hint="可以写佩戴方式、动作、背景、光线和已知尺寸。"><textarea className="textarea" rows={5} maxLength={4000} disabled={controlsLocked} value={prompt} onChange={e => setPrompt(e.target.value)} placeholder="例如：耳垂侧面特写，头发别到耳后，米白背景，自然柔光。商品直径 8mm，保留原有蝴蝶结与珍珠结构。" /></Field>
        </div>
        <div className="tryon-generate"><div className="help">每次 1 张 · 使用本人 AI 生图额度<br />参考图有助于保持一致，生成后请核对商品还原。</div><button className="btn btn-primary" disabled={!canGenerate} onClick={generate}><Sparkles />{task.running ? '任务进行中…' : '生成穿戴照'}</button>{!reference && <span className="help">先选择一张固定模特参考图</span>}{reference && !productType.trim() && !prompt.trim() && <span className="help">填写商品类型或拍摄要求，也可以先 AI 识别</span>}</div>
      </aside>
    </div>
    {picking && <TryOnAssetPicker onClose={() => setPicking(false)} onPick={changeSources} />}
    {saving && <SaveDialog tryon item={saving} onClose={() => setSaving(null)} onSaved={() => { setSaving(null); loadResults() }} />}
  </>
}
