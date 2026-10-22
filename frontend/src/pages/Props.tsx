import { useCallback, useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Archive, ArchiveRestore, Armchair, Check, ImagePlus, Lock, Plus, Search, Sparkles, Trash2, X } from 'lucide-react'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { useJobs, jobRunning } from '../lib/jobs'
import { ANGLES, PROP_KINDS, PROP_VIEWS, propApi, sizeText, type Prop, type PropInput, type PropRef, type PropView } from '../lib/props'
import { Drawer, Empty, Field, Loading, Pill, Topbar } from '../components/ui'
import { useLightbox } from '../components/Lightbox'

// 道具库：桌面、托盘、布料、花植……每个道具是一张设定卡，锁定一张正面参考图（可补左右、俯视等角度），
// 场景的布局框引用道具，定场图照锁定图画，换款时道具长得一样。
export function Props() {
  const { can } = useAuth()
  const toast = useToast()
  const [sp, setSp] = useSearchParams()
  const [kind, setKind] = useState('')
  const [q, setQ] = useState('')
  const [archived, setArchived] = useState(false)
  const [items, setItems] = useState<Prop[] | null>(null)
  const [error, setError] = useState('')
  const [editing, setEditing] = useState<Prop | 'new' | null>(null)
  const editable = can('creative.template')
  const load = useCallback(() => {
    setError('')
    propApi.list({ kind: kind || undefined, archived, q: q.trim() || undefined }).then(setItems).catch(e => { setError(errText(e)); setItems([]) })
  }, [kind, archived, q])
  useEffect(() => { const t = setTimeout(load, 180); return () => clearTimeout(t) }, [load])
  const openId = Number(sp.get('prop')) || 0
  useEffect(() => { if (openId) propApi.get(openId).then(setEditing).catch(e => toast(errText(e), 'error')) }, [openId, toast])
  const open = (p: Prop | 'new') => { setEditing(p); if (p !== 'new') setSp({ prop: String(p.id) }, { replace: true }) }
  const close = () => { setEditing(null); setSp({}, { replace: true }) }

  return <>
    <Topbar title="道具库" sub="台面、托盘、布料、花植等道具的设定卡；锁定参考图后，场景里每次画出来都是同一件">
      {editable && <button className="btn btn-primary" onClick={() => open('new')}><Plus />新建道具</button>}
    </Topbar>
    <div className="scroll"><main className="content">
      <div className="toolbar">
        <select className="input" style={{ width: 130 }} value={kind} onChange={e => setKind(e.target.value)} aria-label="种类">
          <option value="">全部种类</option>{Object.entries(PROP_KINDS).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
        </select>
        <label className="field" style={{ width: 220 }}><Search /><input placeholder="搜道具名、外观" value={q} onChange={e => setQ(e.target.value)} /></label>
        <div className="segmented"><button className={!archived ? 'active' : ''} onClick={() => setArchived(false)}>使用中</button><button className={archived ? 'active' : ''} onClick={() => setArchived(true)}>已归档</button></div>
        <span className="spacer" /><span className="t3 fs-12">{items ? `${items.length} 个道具` : ''}</span>
      </div>
      {!items ? <Loading /> : error ? <section className="panel"><Empty title="道具库加载失败" desc={error}><button className="btn btn-secondary" onClick={load}>重试</button></Empty></section>
        : !items.length ? <section className="panel"><Empty title={archived ? '没有归档的道具' : q || kind ? '没有符合条件的道具' : '还没有道具'} desc={archived ? undefined : '先写清楚外观和真实尺寸，再上传实拍或让 AI 生成候选，选一张锁定。'}>{editable && !archived && <button className="btn btn-secondary" onClick={() => open('new')}><Plus />新建道具</button>}</Empty></section>
          : <div className="scene-grid prop-grid">{items.map(p => <button key={p.id} type="button" className="scene-card" onClick={() => open(p)}>
            <div className="scene-cover prop-cover">{p.cover_url ? <img src={p.cover_url} alt="" loading="lazy" /> : <span className="scene-cover-blank"><Armchair /></span>}
              {!p.cover_url && <span className="lib-ver">还没锁定</span>}
            </div>
            <div className="scene-info">
              <div className="row" style={{ gap: 6 }}><b className="ellipsis">{p.name}</b><Pill kind="pill-neutral" dot={false}>{PROP_KINDS[p.kind] || p.kind}</Pill>{p.archived && <Pill kind="pill-neutral" dot={false}>已归档</Pill>}</div>
              <p className="scene-desc">{p.description}</p>
              <div className="inline fs-12 t3"><span className="mono">{sizeText(p.size_cm)}</span>
                {Object.keys(p.locked_views).length > 0 && <><span className="sep" /><span>{(Object.keys(p.locked_views) as PropView[]).map(v => PROP_VIEWS[v]).join('、')}</span></>}
              </div>
            </div>
          </button>)}</div>}
    </main></div>
    {editing && <PropDrawer key={editing === 'new' ? 'new' : editing.id} prop={editing === 'new' ? undefined : editing} onClose={close}
      onSaved={p => { setEditing(p); setSp({ prop: String(p.id) }, { replace: true }); load() }} onDeleted={() => { close(); load() }} />}
  </>
}

// 关掉抽屉再打开还能接着看生成进度
const running = new Map<number, number>()

function PropDrawer({ prop, onClose, onSaved, onDeleted }: { prop?: Prop; onClose: () => void; onSaved: (p: Prop) => void; onDeleted: () => void }) {
  const { me, can } = useAuth()
  const toast = useToast()
  const lightbox = useLightbox()
  const shops = (me?.shops || []).filter(s => !s.archived)
  const editable = can('creative.template') && !prop?.archived
  const init = (p?: Prop): PropInput => ({ name: p?.name || '', kind: p?.kind || 'table', description: p?.description || '', size_cm: { ...(p?.size_cm || {}) }, shop_id: p?.shop_id ?? null })
  const [v, setV] = useState<PropInput>(init(prop))
  const [busy, setBusy] = useState(false)
  const [count, setCount] = useState(2)
  const [extra, setExtra] = useState('')
  const [angles, setAngles] = useState<PropView[]>(['left', 'top'])
  const [upView, setUpView] = useState<PropView>('main')
  const [jobId, setJobId] = useState(prop ? running.get(prop.id) || 0 : 0)
  const input = useRef<HTMLInputElement>(null)
  const jobs = useJobs(jobId ? [jobId] : [])
  const job = jobId ? jobs[jobId] : undefined
  const live = !!jobId && (!job || jobRunning(job))
  useEffect(() => {
    if (!prop || !job || jobRunning(job)) return
    running.delete(prop.id); setJobId(0)
    if (job.status === 'failed') toast(job.message || '生成失败', 'error')
    else toast('候选图好了，选一张锁定', 'ok')
    propApi.get(prop.id).then(onSaved).catch(() => {})
  }, [job]) // eslint-disable-line react-hooks/exhaustive-deps
  const track = (id: number) => { if (prop) running.set(prop.id, id); setJobId(id) }

  const dirty = !prop || JSON.stringify(init(prop)) !== JSON.stringify(v)
  const size = v.size_cm
  const sizeOk = !!(size.w || size.d || size.h)
  const setSize = (k: 'w' | 'd' | 'h', s: string) => { const n = Number(s); const next = { ...size }; if (s && n > 0) next[k] = n; else delete next[k]; setV({ ...v, size_cm: next }) }
  const act = async (fn: () => Promise<Prop | void>, ok?: string) => {
    setBusy(true)
    try { const p = await fn(); if (ok) toast(ok, 'ok'); if (p) onSaved(p); else if (prop) onSaved(await propApi.get(prop.id)) } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const save = () => act(() => prop ? propApi.patch(prop.id, { ...v, name: v.name.trim(), description: v.description.trim() }) : propApi.create({ ...v, name: v.name.trim(), description: v.description.trim() }), prop ? '道具已保存' : '道具已建好，接着上传或生成参考图')
  const archive = () => prop && act(() => propApi.patch(prop.id, { archived: !prop.archived }), prop.archived ? '已恢复使用' : '已归档')
  const remove = async () => {
    if (!prop || !confirm(`删除道具「${prop.name}」？参考图一起删掉；有场景在用时只会归档。`)) return
    setBusy(true)
    try { const r = await propApi.remove(prop.id); if (r.deleted) { toast('道具已删除', 'ok'); onDeleted() } else { toast('有场景在用，已改为归档', 'ok'); onSaved(await propApi.get(prop.id)) } }
    catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const upload = (files: FileList) => {
    const f = files[0]
    if (!f || !/image\/(jpeg|png|webp)/.test(f.type)) return toast('只认 jpg、png、webp 图片', 'error')
    if (prop) act(() => propApi.upload(prop.id, f, upView, false).then(() => undefined), `已传成${PROP_VIEWS[upView]}候选`)
  }
  const generate = () => prop && act(async () => { const r = await propApi.generate(prop.id, count, extra.trim()); track(r.job_id) }, `开始生成 ${count} 张候选`)
  const addViews = () => prop && act(async () => { const r = await propApi.views(prop.id, angles); track(r.job_id) }, '开始补角度')

  const refs = (prop?.refs || []).filter(r => r.status !== 'discarded')
  const locked = refs.filter(r => r.status === 'locked')
  const cands = refs.filter(r => r.status === 'candidate')
  const mainLocked = locked.some(r => r.view === 'main')
  const view = (list: PropRef[], i: number) => lightbox(list.map(r => ({ src: r.url, title: `${prop?.name} · ${PROP_VIEWS[r.view]}${r.status === 'locked' ? '（锁定）' : '（候选）'}`, meta: `${r.width}×${r.height}${r.source === 'ai' ? ' · AI 生成' : ' · 上传'}`, download: '' })), i)

  return <Drawer title={prop ? <span className="inline"><Armchair size={16} />{prop.name}</span> : '新建道具'} width={680} onClose={onClose} foot={<>
    {prop && can('creative.template') && <button className="btn btn-ghost" disabled={busy || live} onClick={archive}>{prop.archived ? <ArchiveRestore /> : <Archive />}{prop.archived ? '恢复使用' : '归档'}</button>}
    {prop && can('creative.template') && <button className="btn btn-ghost danger-text" disabled={busy || live} onClick={remove}><Trash2 />删除</button>}
    <span className="grow" />
    <button className="btn btn-secondary" onClick={onClose}>{dirty && editable ? '取消' : '关闭'}</button>
    {editable && <button className="btn btn-primary" disabled={busy || !v.name.trim() || !v.description.trim() || !sizeOk || !dirty} onClick={save}>{busy ? '保存中…' : prop ? '保存' : '建好，下一步做参考图'}</button>}
  </>}>
    <div className="stack">
      <div className="form-row">
        <Field label="道具名" req><input className="input" maxLength={40} autoFocus={!prop} disabled={!editable} placeholder="如：胡桃木圆托盘、米白亚麻布" value={v.name} onChange={e => setV({ ...v, name: e.target.value })} /></Field>
        <Field label="种类"><select className="input" disabled={!editable} value={v.kind} onChange={e => setV({ ...v, kind: e.target.value })}>{Object.entries(PROP_KINDS).map(([k, n]) => <option key={k} value={k}>{n}</option>)}</select></Field>
        <Field label="谁能用"><select className="input" disabled={!editable} value={v.shop_id || 0} onChange={e => setV({ ...v, shop_id: Number(e.target.value) || null })}>
          <option value={0}>所有店铺通用</option>{shops.map(s => <option key={s.id} value={s.id}>只给 {s.name}</option>)}
        </select></Field>
      </div>
      <Field label="外观描述" req hint="材质、颜色、形状、表面纹理；AI 生成候选和定场图都照这段写"><textarea className="textarea" rows={3} maxLength={1000} disabled={!editable} value={v.description} onChange={e => setV({ ...v, description: e.target.value })} placeholder="如：深胡桃木圆形托盘，边缘略高 1cm，表面哑光、木纹清楚" /></Field>
      <div className="form-field">
        <span className="label req">真实尺寸（厘米，至少填一项）</span>
        <div className="prop-size">
          {(['w', 'd', 'h'] as const).map(k => <label key={k} className="field prop-size-in"><span className="t3">{{ w: '宽', d: '深', h: '高' }[k]}</span><input type="number" min={0} step={0.1} disabled={!editable} value={size[k] ?? ''} onChange={e => setSize(k, e.target.value)} /></label>)}
          <span className="help">定场图按尺寸和商品比大小，填准了首饰才不会显得过大过小</span>
        </div>
      </div>

      {prop && <>
        <div className="form-section-title">锁定的参考图</div>
        {locked.length ? <div className="prop-locked">{locked.map((r, i) => <div key={r.id} className="prop-ref on">
          <button type="button" className="zoomable" onClick={() => view(locked, i)}><img src={r.url} alt="" /></button>
          <div className="prop-ref-meta"><Lock size={12} />{PROP_VIEWS[r.view]}<span className="grow" />{editable && <button className="btn btn-ghost btn-icon btn-sm" title="取消锁定并丢弃" aria-label="丢弃" disabled={busy} onClick={() => confirm(`丢掉锁定的${PROP_VIEWS[r.view]}图？`) && act(() => propApi.discard(prop.id, r.id))}><X /></button>}</div>
        </div>)}</div> : <div className="t3 fs-12">还没有。先锁定一张正面图，场景定场图才会照它画。</div>}

        {cands.length > 0 && <>
          <div className="form-section-title">候选<span className="t3 fs-12" style={{ fontWeight: 400 }}>　选一张锁定；同一个角度锁新的会替掉旧的</span></div>
          <div className="prop-cands">{cands.map((r, i) => <div key={r.id} className="prop-ref">
            <button type="button" className="zoomable" onClick={() => view(cands, i)}><img src={r.url} alt="" /></button>
            <div className="prop-ref-meta"><span>{PROP_VIEWS[r.view]}</span><span className="t4">{r.source === 'ai' ? 'AI' : '上传'}</span><span className="grow" />
              {editable && <>
                <button className="btn btn-secondary btn-sm" disabled={busy} onClick={() => act(() => propApi.lock(prop.id, r.id), `已锁定为${PROP_VIEWS[r.view]}图`)}><Check />锁定</button>
                <button className="btn btn-ghost btn-icon btn-sm" title="丢弃" aria-label="丢弃" disabled={busy} onClick={() => act(() => propApi.discard(prop.id, r.id))}><X /></button>
              </>}
            </div>
          </div>)}</div>
        </>}

        {editable && <>
          <div className="form-section-title">做参考图</div>
          {live && <div className="views-job fs-12 row between"><span className="inline"><Sparkles size={14} />{job?.kind === 'props.views' ? '补角度' : '生成候选'} · {job?.message || '排队中'}</span><span className="mono">{job?.progress || 0}%</span></div>}
          <div className="prop-make">
            <div className="stack" style={{ gap: 8 }}>
              <b className="fs-13">AI 生成正面候选</b>
              <textarea className="textarea" rows={2} maxLength={400} value={extra} onChange={e => setExtra(e.target.value)} placeholder="补充要求（可空）：如 纯白背景、俯拍 30°" />
              <div className="row" style={{ gap: 8 }}>
                <select className="input input-sm" style={{ width: 90 }} value={count} onChange={e => setCount(Number(e.target.value))}>{[1, 2, 3, 4].map(n => <option key={n} value={n}>{n} 张</option>)}</select>
                <button className="btn btn-primary btn-sm" disabled={busy || live || !v.description.trim()} onClick={generate}><Sparkles />生成</button>
              </div>
            </div>
            <div className="stack" style={{ gap: 8 }}>
              <b className="fs-13">照锁定正面补角度</b>
              <div className="inline">{ANGLES.map(a => <label key={a} className="filter-opt"><input type="checkbox" className="check" checked={angles.includes(a)} onChange={e => setAngles(e.target.checked ? [...angles, a] : angles.filter(x => x !== a))} />{PROP_VIEWS[a]}{prop.locked_views[a] ? <span className="t4">（已有）</span> : null}</label>)}</div>
              <button className="btn btn-secondary btn-sm" style={{ alignSelf: 'flex-start' }} disabled={busy || live || !mainLocked || !angles.length} onClick={addViews} title={mainLocked ? '' : '先锁定一张正面图'}><Sparkles />补 {angles.length} 个角度</button>
            </div>
            <div className="stack" style={{ gap: 8 }}>
              <b className="fs-13">上传实拍</b>
              <select className="input input-sm" value={upView} onChange={e => setUpView(e.target.value as PropView)}>{Object.entries(PROP_VIEWS).map(([k, n]) => <option key={k} value={k}>{n}</option>)}</select>
              <button className="btn btn-secondary btn-sm" style={{ alignSelf: 'flex-start' }} disabled={busy} onClick={() => input.current?.click()}><ImagePlus />选图片</button>
              <input ref={input} type="file" accept="image/jpeg,image/png,image/webp" hidden onChange={e => { if (e.target.files) upload(e.target.files); e.target.value = '' }} />
            </div>
          </div>
        </>}
      </>}
    </div>
  </Drawer>
}
