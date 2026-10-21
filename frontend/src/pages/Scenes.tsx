import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Archive, ArchiveRestore, ImagePlus, Mountain, Plus, Search, Shirt, Star, Trash2, X } from 'lucide-react'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { useCategories } from '../lib/categories'
import { useListingRefs } from '../lib/listings'
import { sceneApi, SCENE_SOURCE, type Binding, type Scene, type SceneInput, type ShopBinding } from '../lib/scenes'
import { Drawer, Empty, Field, Loading, Pill, Topbar } from '../components/ui'
import { useLightbox } from '../components/Lightbox'

// 场景库：背景台面、上身穿搭、光线色调、道具和参考图。给款（店铺版本）指定场景，同系列用同一个，出的图就一致。
export function Scenes() {
  const { me, can } = useAuth()
  const toast = useToast()
  const [sp, setSp] = useSearchParams()
  const { items: cats } = useCategories()
  const shops = (me?.shops || []).filter(s => !s.archived)
  const [shop, setShop] = useState(0)
  const [cat, setCat] = useState('')
  const [q, setQ] = useState('')
  const [archived, setArchived] = useState(false)
  const [items, setItems] = useState<Scene[] | null>(null)
  const [error, setError] = useState('')
  const [editing, setEditing] = useState<Scene | 'new' | null>(null)
  const editable = can('creative.template')
  const load = useCallback(() => {
    setError('')
    sceneApi.list({ shop_id: shop || undefined, category: cat || undefined, archived, q: q.trim() || undefined })
      .then(setItems).catch(e => { setError(errText(e)); setItems([]) })
  }, [shop, cat, archived, q])
  useEffect(() => { const t = setTimeout(load, 180); return () => clearTimeout(t) }, [load])
  // 地址里带 ?scene= 时直接打开
  const openId = Number(sp.get('scene')) || 0
  useEffect(() => {
    if (!openId) return
    sceneApi.get(openId).then(setEditing).catch(e => toast(errText(e), 'error'))
  }, [openId, toast])
  const open = (s: Scene | 'new') => { setEditing(s); if (s !== 'new') setSp({ scene: String(s.id) }, { replace: true }) }
  const close = () => { setEditing(null); setSp({}, { replace: true }) }
  const shopName = (id: number | null) => id ? shops.find(s => s.id === id)?.name || `店铺 ${id}` : '所有店铺'
  const catName = (key: string) => cats?.find(c => c.key === key)?.name || key

  return <>
    <Topbar title="场景库" sub="给商品配好背景、穿搭和光线；同一系列用同一个场景，出的图风格就一致">
      {editable && <button className="btn btn-primary" onClick={() => open('new')}><Plus />新建场景</button>}
    </Topbar>
    <div className="scroll"><main className="content">
      <div className="toolbar">
        {shops.length > 1 && <select className="input" style={{ width: 150 }} value={shop} onChange={e => setShop(Number(e.target.value))} aria-label="店铺">
          <option value={0}>全部店铺</option>{shops.map(s => <option key={s.id} value={s.id}>{s.name}</option>)}
        </select>}
        {(cats?.length || 0) > 1 && <select className="input" style={{ width: 120 }} value={cat} onChange={e => setCat(e.target.value)} aria-label="品类">
          <option value="">全部品类</option>{cats!.map(c => <option key={c.id} value={c.key}>{c.name}</option>)}
        </select>}
        <label className="field" style={{ width: 220 }}><Search /><input placeholder="搜场景名、描述" value={q} onChange={e => setQ(e.target.value)} /></label>
        <div className="segmented"><button className={!archived ? 'active' : ''} onClick={() => setArchived(false)}>使用中</button><button className={archived ? 'active' : ''} onClick={() => setArchived(true)}>已归档</button></div>
        <span className="spacer" /><span className="t3 fs-12">{items ? `${items.length} 个场景` : ''}</span>
      </div>
      {!items ? <Loading /> : error ? <section className="panel"><Empty title="场景库加载失败" desc={error}><button className="btn btn-secondary" onClick={load}>重试</button></Empty></section>
        : !items.length ? <section className="panel"><Empty title={archived ? '没有归档的场景' : q || cat || shop ? '没有符合条件的场景' : '还没有场景'} desc={archived ? undefined : '一个场景写清楚静物放在什么台面上、模特穿什么、什么光线，再传几张参考图。'}>{editable && !archived && <button className="btn btn-secondary" onClick={() => open('new')}><Plus />新建场景</button>}</Empty></section>
          : <div className="scene-grid">{items.map(s => {
            const cover = s.refs.find(r => r.use === 'still') || s.refs[0]
            return <button key={s.id} type="button" className="scene-card" onClick={() => open(s)}>
              <div className="scene-cover">{cover ? <img src={cover.url} alt="" loading="lazy" /> : <span className="scene-cover-blank"><Mountain /></span>}
                {s.refs.length > 0 && <span className="lib-ver mono">{s.refs.length} 张参考</span>}
              </div>
              <div className="scene-info">
                <div className="row" style={{ gap: 6 }}><b className="ellipsis">{s.name}</b>{s.is_default && <Pill kind="pill-info" dot={false}>默认</Pill>}{s.archived && <Pill kind="pill-neutral" dot={false}>已归档</Pill>}</div>
                <p className="scene-desc">{s.note || s.still || s.wear || '还没写描述'}</p>
                <div className="inline fs-12 t3">
                  <span>{shopName(s.shop_id)}</span><span className="sep" />
                  <span>{s.categories.length ? s.categories.map(catName).join('、') : '全部品类'}</span><span className="sep" />
                  <span>用在 <span className="mono">{s.usage.listings}</span> 款</span>
                  {s.usage.shops > 0 && <><span className="sep" /><span>{s.usage.shops} 个店铺默认</span></>}
                </div>
              </div>
            </button>
          })}</div>}
    </main></div>
    {editing && <SceneDrawer key={editing === 'new' ? 'new' : editing.id} scene={editing === 'new' ? undefined : editing} defaultShop={shop || null}
      onClose={close} onSaved={s => { setEditing(s); setSp({ scene: String(s.id) }, { replace: true }); load() }} onDeleted={() => { close(); load() }} />}
  </>
}

const EMPTY: SceneInput = { name: '', categories: [], still: '', wear: '', light: '', props: '', note: '', shop_id: null }

function SceneDrawer({ scene, defaultShop, onClose, onSaved, onDeleted }: { scene?: Scene; defaultShop: number | null; onClose: () => void; onSaved: (s: Scene) => void; onDeleted: () => void }) {
  const { me, can } = useAuth()
  const toast = useToast()
  const lightbox = useLightbox()
  const { items: cats } = useCategories()
  const shops = (me?.shops || []).filter(s => !s.archived)
  const editable = can('creative.template')
  const [v, setV] = useState<SceneInput>(scene ? { name: scene.name, categories: scene.categories, still: scene.still, wear: scene.wear, light: scene.light, props: scene.props, note: scene.note, shop_id: scene.shop_id } : { ...EMPTY, shop_id: defaultShop })
  const [busy, setBusy] = useState(false)
  const set = (patch: Partial<SceneInput>) => setV(old => ({ ...old, ...patch }))
  const save = async () => {
    setBusy(true)
    try {
      const body = { ...v, name: v.name.trim() }
      const s = scene ? await sceneApi.patch(scene.id, body) : await sceneApi.create(body)
      toast(scene ? '场景已保存' : '场景已建好，可以接着传参考图', 'ok'); onSaved(s)
    } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const reload = () => scene && sceneApi.get(scene.id).then(onSaved).catch(e => toast(errText(e), 'error'))
  const archive = async () => {
    if (!scene) return
    setBusy(true)
    try { onSaved(await sceneApi.patch(scene.id, { archived: !scene.archived })); toast(scene.archived ? '已恢复使用' : '已归档，用这个场景的款会改用店铺默认场景', 'ok') }
    catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const remove = async () => {
    if (!scene || !confirm(`永久删除场景「${scene.name}」？参考图会一起删掉；已经出好的图不受影响。`)) return
    setBusy(true)
    try { await sceneApi.remove(scene.id); toast('场景已删除', 'ok'); onDeleted() } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const dirty = !scene || JSON.stringify({ name: scene.name, categories: scene.categories, still: scene.still, wear: scene.wear, light: scene.light, props: scene.props, note: scene.note, shop_id: scene.shop_id }) !== JSON.stringify(v)
  const toggleCat = (key: string) => set({ categories: v.categories.includes(key) ? v.categories.filter(k => k !== key) : [...v.categories, key] })
  const ro = !editable

  return <Drawer title={scene ? <span className="inline"><Mountain size={16} />{scene.name}{scene.builtin_key && <span className="t4 fs-12">内置</span>}</span> : '新建场景'} width={640} onClose={onClose} foot={<>
    {scene && editable && !scene.is_default && <button className="btn btn-ghost" disabled={busy} onClick={archive}>{scene.archived ? <ArchiveRestore /> : <Archive />}{scene.archived ? '恢复使用' : '归档'}</button>}
    {scene && editable && scene.archived && <button className="btn btn-danger" disabled={busy} onClick={remove}><Trash2 />永久删除</button>}
    <span className="grow" />
    <button className="btn btn-secondary" onClick={onClose}>{dirty && editable ? '取消' : '关闭'}</button>
    {editable && <button className="btn btn-primary" disabled={busy || !v.name.trim() || (!v.still.trim() && !v.wear.trim()) || !dirty} onClick={save}>{busy ? '保存中…' : scene ? '保存' : '建好，下一步传参考图'}</button>}
  </>}>
    <div className="stack">
      <div className="form-row">
        <Field label="场景名" req><input className="input" maxLength={40} autoFocus={!scene} disabled={ro} placeholder="如：冷灰石材、米白画册、窗边针织" value={v.name} onChange={e => set({ name: e.target.value })} /></Field>
        <Field label="用在哪个店铺"><select className="input" disabled={ro} value={v.shop_id || 0} onChange={e => set({ shop_id: Number(e.target.value) || null })}>
          <option value={0}>所有店铺通用</option>{shops.map(s => <option key={s.id} value={s.id}>只给 {s.name}</option>)}
        </select></Field>
      </div>
      {(cats?.length || 0) > 1 && <div className="form-field"><span className="label">适用品类</span>
        <div className="inline">{cats!.map(c => <label key={c.id} className="filter-opt"><input type="checkbox" className="check" disabled={ro} checked={v.categories.includes(c.key)} onChange={() => toggleCat(c.key)} />{c.name}</label>)}<span className="help">都不勾 = 全部品类</span></div>
      </div>}
      <Field label="静物：背景和台面" hint="主图、细节图、规格图里商品放在什么上面、背景是什么。写具体的材质和颜色。"><textarea className="textarea" rows={3} maxLength={600} disabled={ro} value={v.still} onChange={e => set({ still: e.target.value })} placeholder="如：哑光中灰石材台面，有细微天然纹理，背景是同色调的灰墙，干净不杂乱" /></Field>
      <Field label="上身：穿搭和背景" hint="模特图里穿什么、站在什么环境。不写模特长相，长相在模特库。"><textarea className="textarea" rows={3} maxLength={600} disabled={ro} value={v.wear} onChange={e => set({ wear: e.target.value })} placeholder="如：炭灰色细针织圆领毛衣，深蓝灰纯色背景，不露其他首饰" /></Field>
      <div className="form-row">
        <Field label="光线和色调"><textarea className="textarea" rows={2} maxLength={600} disabled={ro} value={v.light} onChange={e => set({ light: e.target.value })} placeholder="如：左上方柔和窗光，冷灰暗调，低饱和" /></Field>
        <Field label="道具（可空）"><textarea className="textarea" rows={2} maxLength={600} disabled={ro} value={v.props} onChange={e => set({ props: e.target.value })} placeholder="如：一小枝干花放在画面边缘，不抢主体" /></Field>
      </div>
      <Field label="说明（给同事看的，不进提示词）"><input className="input" maxLength={300} disabled={ro} value={v.note} onChange={e => set({ note: e.target.value })} placeholder="如：秋冬系列用；珍珠项链配这个最好看" /></Field>
      {scene && <>
        <RefSection scene={scene} use="still" title="静物参考图" hint="AI 照着这几张的台面、光线、色调来画；最多 4 张" editable={editable && !scene.archived} onChange={reload} onView={i => lightbox(scene.refs.filter(r => r.use === 'still').map(r => ({ src: r.url, title: `${scene.name} · 静物参考` })), i)} />
        <RefSection scene={scene} use="wear" title="上身参考图" hint="AI 照着这几张的穿搭、背景来画模特图；最多 4 张" editable={editable && !scene.archived} onChange={reload} onView={i => lightbox(scene.refs.filter(r => r.use === 'wear').map(r => ({ src: r.url, title: `${scene.name} · 上身参考` })), i)} />
        {!scene.archived && <SceneUsage scene={scene} onChange={reload} />}
      </>}
    </div>
  </Drawer>
}

function RefSection({ scene, use, title, hint, editable, onChange, onView }: { scene: Scene; use: 'still' | 'wear'; title: string; hint: string; editable: boolean; onChange: () => void; onView: (i: number) => void }) {
  const toast = useToast()
  const input = useRef<HTMLInputElement>(null)
  const [busy, setBusy] = useState(false)
  const refs = scene.refs.filter(r => r.use === use)
  const add = async (files: FileList) => {
    const list = Array.from(files).filter(f => /image\/(jpeg|png|webp)/.test(f.type)).slice(0, 4 - refs.length)
    if (!list.length) return toast('只认 jpg、png、webp 图片', 'error')
    setBusy(true)
    try { for (const f of list) await sceneApi.addRef(scene.id, f, use); toast(`已加 ${list.length} 张`, 'ok'); onChange() }
    catch (e) { toast(errText(e), 'error'); onChange() } finally { setBusy(false); if (input.current) input.current.value = '' }
  }
  const remove = async (id: number) => {
    try { await sceneApi.removeRef(scene.id, id); onChange() } catch (e) { toast(errText(e), 'error') }
  }
  return <div className="form-field">
    <span className="label inline">{use === 'still' ? <Mountain size={14} /> : <Shirt size={14} />}{title}<span className="help">{hint}</span></span>
    <div className="scene-refs">
      {refs.map((r, i) => <div key={r.id} className="scene-ref">
        <button type="button" className="zoomable" onClick={() => onView(i)}><img src={r.url} alt="" loading="lazy" /></button>
        {editable && <button type="button" className="scene-ref-x" title="删除这张参考图" aria-label="删除" onClick={() => remove(r.id)}><X /></button>}
      </div>)}
      {editable && refs.length < 4 && <button type="button" className="scene-ref scene-ref-add" disabled={busy} onClick={() => input.current?.click()}><ImagePlus />{busy ? '上传中…' : '加参考图'}</button>}
      {!editable && !refs.length && <span className="t4 fs-12">没有参考图</span>}
    </div>
    <input ref={input} type="file" accept="image/jpeg,image/png,image/webp" multiple hidden onChange={e => e.target.files && add(e.target.files)} />
  </div>
}

// 这个场景用在哪些款：勾选店铺版本指定场景；也可以设成某个店铺的默认
function SceneUsage({ scene, onChange }: { scene: Scene; onChange: () => void }) {
  const { me, can } = useAuth()
  const toast = useToast()
  const refs = useListingRefs()
  const shops = (me?.shops || []).filter(s => !s.archived && (!scene.shop_id || s.id === scene.shop_id))
  const [binds, setBinds] = useState<Binding[] | null>(null)
  const [shopBinds, setShopBinds] = useState<ShopBinding[]>([])
  const [q, setQ] = useState('')
  const [busy, setBusy] = useState(false)
  const [picked, setPicked] = useState<Set<number>>(new Set())
  const candidates = useMemo(() => (refs || []).filter(r => (!scene.shop_id || r.shop_id === scene.shop_id) && (!scene.categories.length || scene.categories.includes(r.product.category?.key || ''))), [refs, scene])
  const loadBinds = useCallback(() => {
    if (!candidates.length) { setBinds([]); return }
    sceneApi.bindings(candidates.map(r => r.id)).then(r => { setBinds(r.listings); setShopBinds(r.shops) }).catch(() => setBinds([]))
  }, [candidates])
  useEffect(() => { loadBinds() }, [loadBinds])
  const run = can('creative.run'), manage = can('creative.template')
  const byListing = new Map((binds || []).map(b => [b.listing_id, b]))
  const shopName = (id: number) => shops.find(s => s.id === id)?.name || `店铺 ${id}`
  const shown = candidates.filter(r => !q || r.product.name.includes(q))
  const using = shown.filter(r => byListing.get(r.id)?.scene_id === scene.id)
  const others = shown.filter(r => byListing.get(r.id)?.scene_id !== scene.id)
  const bind = async (ids: number[], sceneId: number | null) => {
    setBusy(true)
    try { await sceneApi.bind({ listing_ids: ids, scene_id: sceneId }); toast(sceneId ? `${ids.length} 款改用「${scene.name}」，下次新出的图生效` : '已取消单独指定，改用店铺默认', 'ok'); setPicked(new Set()); loadBinds(); onChange() }
    catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const shopDefault = async (shopId: number, on: boolean) => {
    setBusy(true)
    try { await sceneApi.bind({ shop_id: shopId, scene_id: on ? scene.id : null }); toast(on ? `「${shopName(shopId)}」没单独指定的款都用这个场景` : '已取消店铺默认', 'ok'); loadBinds(); onChange() }
    catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const row = (r: (typeof candidates)[number], mine: boolean) => {
    const b = byListing.get(r.id)
    return <label key={r.id} className={'scene-use-row' + (mine ? ' on' : '')}>
      {run && !mine && <input type="checkbox" className="check" checked={picked.has(r.id)} onChange={e => setPicked(old => { const n = new Set(old); if (e.target.checked) n.add(r.id); else n.delete(r.id); return n })} />}
      {r.product.cover_url ? <img className="cover" src={r.product.cover_url} alt="" /> : <span className="cover" />}
      <span className="grow ellipsis">{r.product.name}<span className="t4 fs-12"> · {shopName(r.shop_id)}</span></span>
      {b && !mine && <span className="t3 fs-12 ellipsis" style={{ maxWidth: 180 }}>现在：{b.scene_name || '—'}（{SCENE_SOURCE[b.source]}）</span>}
      {mine && <span className="t3 fs-12">{b ? SCENE_SOURCE[b.source] : ''}</span>}
      {mine && run && b?.source === 'listing' && <button type="button" className="btn btn-ghost btn-sm" disabled={busy} onClick={e => { e.preventDefault(); bind([r.id], null) }}>取消</button>}
    </label>
  }
  return <div className="form-field">
    <span className="label">用在哪些款</span>
    {manage && shops.length > 0 && <div className="inline fs-12">
      <span className="t3">设成店铺默认：</span>
      {shops.map(s => { const on = shopBinds.some(b => b.shop_id === s.id && b.scene_id === scene.id); return <button key={s.id} type="button" className={'btn btn-sm ' + (on ? 'btn-secondary' : 'btn-ghost')} disabled={busy} onClick={() => shopDefault(s.id, !on)}>{on && <Star />}{s.name}{on ? '（已是默认）' : ''}</button> })}
    </div>}
    {!refs || !binds ? <Loading /> : !candidates.length ? <div className="t4 fs-12">没有能用这个场景的款（看看店铺和品类设置）</div> : <>
      <label className="field"><Search /><input placeholder="搜款名" value={q} onChange={e => setQ(e.target.value)} /></label>
      <div className="scene-use-list">
        {using.length > 0 && <div className="scene-use-head">正在用（{using.length}）</div>}
        {using.map(r => row(r, true))}
        {others.length > 0 && <div className="scene-use-head">其他款（{others.length}）{run && picked.size > 0 && <button type="button" className="btn btn-primary btn-sm" disabled={busy} onClick={() => bind([...picked], scene.id)}>勾选的 {picked.size} 款用这个场景</button>}</div>}
        {others.map(r => row(r, false))}
      </div>
      <div className="help">改场景只影响之后新出的套装；已经出好的图不变，要换就在「AI 出图」里再出一套。</div>
    </>}
  </div>
}
