import { useCallback, useEffect, useState } from 'react'
import { Sparkles, Download, Eye, Play, Plus, Trash2, RefreshCw, Star, History } from 'lucide-react'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { ApiError } from '../lib/api'
import type { Product } from '../lib/types'
import { Pill, Loading, Empty, Drawer, Field } from './ui'
import { ItemCard, CompareView, ReviewDetail, TextRows, JudgeBar } from './CreativeShared'
import '../styles/creative.css'
import { DETAIL_SLOTS, HERO_STYLE, MAIN_SLOTS, SET_STATUS, creativeApi, itemOk, mockOn, slotName, type CreativeItem, type CreativeSet, type SetParams } from '../lib/creative'

// 商品详情「AI 出图」：每个店铺版本一套或多套 AI 主图和详情页，逐张复核后导出
export function CreativeTab({ product }: { product: Product }) {
  const { me } = useAuth()
  const listings = product.listings
  const [lid, setLid] = useState(listings[0]?.id || 0)
  const shopName = (id: number) => me?.shops.find(s => s.id === id)?.name || `店铺 ${id}`
  if (!listings.length) return <section className="panel"><Empty title="还没有店铺版本" desc="先在「店铺版本」里加一个店，再来出 AI 图。" /></section>
  return (
    <div className="stack">
      {listings.length > 1 && <div className="segmented">{listings.map(l => <button key={l.id} className={l.id === lid ? 'active' : ''} onClick={() => setLid(l.id)}>{shopName(l.shop_id)}</button>)}</div>}
      <ListingCreative key={lid} listingId={lid} product={product} />
    </div>
  )
}

function ListingCreative({ listingId, product }: { listingId: number; product: Product }) {
  const { can } = useAuth()
  const toast = useToast()
  const [sets, setSets] = useState<CreativeSet[] | null>(null)
  const [setId, setSetId] = useState(0)
  const [creating, setCreating] = useState(false)
  const [open, setOpen] = useState<number | null>(null)
  const [missing, setMissing] = useState<string[]>([])
  const load = useCallback(() => creativeApi.sets(listingId).then(r => {
    setSets(r.items || [])
    setSetId(id => (r.items || []).some(s => s.id === id) ? id : r.items?.[0]?.id || 0)
  }).catch(e => { toast(errText(e), 'error'); setSets([]) }), [listingId, toast])
  useEffect(() => { load() }, [load])
  const set = sets?.find(s => s.id === setId)
  // 生成中每 3 秒刷新
  const busy = set && (set.status === 'generating' || (set.jobs || []).some(j => j.status === 'queued' || j.status === 'running'))
  useEffect(() => {
    if (!busy || mockOn()) return
    const t = setInterval(load, 3000)
    return () => clearInterval(t)
  }, [busy, load])

  if (!sets) return <Loading />
  const run = can('creative.run'), review = can('creative.review')
  if (!set) return (
    <section className="panel">
      <Empty title="还没有 AI 出图" desc="按模板出主图 5 张（首图 A/B 两版）、SKU 图和详情页 9 屏；每张图都会自动复核，首图和模特图要人看过才能用。">
        {run && <button className="btn btn-primary" onClick={() => setCreating(true)}><Sparkles />开始 AI 出图</button>}
      </Empty>
      {creating && <CreateDrawer listingId={listingId} product={product} onClose={() => setCreating(false)} onCreated={s => { setCreating(false); setSetId(s.id); load() }} />}
    </section>
  )

  const cur = set.items.filter(i => !i.superseded)
  const ok = cur.filter(itemOk).length
  const pending = cur.filter(i => !i.human && i.route === 'manual').length
  const st = SET_STATUS[set.status] || SET_STATUS.draft
  const act = async (f: () => Promise<unknown>, msg: string) => {
    try { await f(); toast(msg, 'ok'); load() } catch (e) { toast(errText(e), 'error') }
  }
  const doExport = async () => {
    try {
      await creativeApi.exportSet(set.id)
      setMissing([]); toast('已打包', 'ok'); load()
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) { setMissing([e.message]); return }
      toast(errText(e), 'error')
    }
  }
  const item = set.items.find(i => i.id === open)
  const grid = (slots: string[]) => {
    const list = cur.filter(i => slots.includes(i.slot)).sort((a, b) => slots.indexOf(a.slot) - slots.indexOf(b.slot) || a.variant.localeCompare(b.variant) || a.seq - b.seq)
    return list.length ? <div className="cr-grid">{list.map(i => <ItemCard key={i.id} it={i} onOpen={() => setOpen(i.id)} active={open === i.id} />)}</div> : <div className="t3 fs-12">{busy ? '排队中…' : '这一部分没生成'}</div>
  }
  return (
    <>
      <section className="panel">
        <div className="panel-head">
          <div className="titles">
            <div className="panel-title inline">AI 出图
              {sets.length > 1 ? <select className="input input-sm" style={{ width: 90 }} value={set.id} onChange={e => setSetId(Number(e.target.value))}>{sets.map(s => <option key={s.id} value={s.id}>第 {s.version} 套</option>)}</select> : <span className="t3 fs-12">第 {set.version} 套</span>}
              <Pill kind={st.pill}>{st.label}</Pill>
            </div>
            <div className="panel-sub">可用 {ok} / {cur.length} 张 · 待人工 {pending} 张 · 已生成 {set.images_used} / {set.images_allowance} 次{set.params.hero_style ? ` · 首屏${HERO_STYLE[set.params.hero_style]}` : ''}</div>
          </div>
          {run && <button className="btn btn-ghost btn-sm" onClick={() => setCreating(true)}><Plus />再出一套</button>}
          {review && !set.glanced_at && <button className="btn btn-secondary btn-sm" onClick={() => act(() => creativeApi.glance(set.id), '已记录整套过目')}><Eye />整套过目</button>}
          {run && set.status === 'paused_limit' && <button className="btn btn-secondary btn-sm" onClick={() => { if (confirm(`这一套已经生成 ${set.images_used} 次，继续会再给一轮额度。确定继续？`)) act(() => creativeApi.cont(set.id), '已继续生成') }}><Play />确认继续</button>}
          {run && can('export.run') && <button className="btn btn-primary btn-sm" disabled={!!busy} onClick={doExport}><Download />{set.exported_at ? '重新导出' : '导出'}</button>}
          {set.export_file && <a className="btn btn-secondary btn-sm" href={`/api/creative/sets/${set.id}/export`}><Download />下载 zip</a>}
        </div>
        <div className="panel-pad stack" style={{ gap: 12 }}>
          {(set.jobs || []).filter(j => j.status === 'queued' || j.status === 'running').map(j => (
            <div key={j.id} className="stack" style={{ gap: 6 }}>
              <div className="row between fs-12"><span>{j.kind === 'creative.detail' ? '详情页' : j.kind === 'creative.main' ? '主图' : '单张'} · {j.message || '排队中'}</span><span className="mono">{j.progress}%</span></div>
              <div className="progress"><span style={{ width: `${j.progress}%` }} /></div>
            </div>
          ))}
          {set.status === 'paused_limit' && <div className="status-note">已达每套 {set.images_allowance} 次生成上限，暂停了。看过已有的图再决定要不要继续。</div>}
          {missing.length > 0 && <div className="status-note">还不能导出：{missing.join('；')}</div>}
          {set.glanced_at && <div className="fs-12 t3">整套已过目 · {new Date(set.glanced_at).toLocaleString('zh-CN')}</div>}
          <div className="stack" style={{ gap: 8 }}><div className="sub-title">主图</div>{grid(MAIN_SLOTS)}</div>
          <div className="stack" style={{ gap: 8 }}><div className="sub-title">详情页</div>{grid(DETAIL_SLOTS)}</div>
        </div>
      </section>
      {item && <ItemDrawer it={item} history={set.items.filter(i => i.slot === item.slot && i.seq === item.seq && i.variant === item.variant && i.id !== item.id)} onClose={() => setOpen(null)} onChanged={load} />}
      {creating && <CreateDrawer listingId={listingId} product={product} onClose={() => setCreating(false)} onCreated={s => { setCreating(false); setSetId(s.id); load() }} />}
    </>
  )
}

function ItemDrawer({ it, history, onClose, onChanged }: { it: CreativeItem; history: CreativeItem[]; onClose: () => void; onChanged: () => void }) {
  const { can } = useAuth()
  const toast = useToast()
  const [note, setNote] = useState('')
  const [old, setOld] = useState<CreativeItem | null>(null)
  const show = old || it
  const redraw = async () => {
    try { await creativeApi.redraw(it.id, note.trim()); toast('已开始重画', 'ok'); setNote(''); onChanged() } catch (e) { toast(errText(e), 'error') }
  }
  const chooseA = async () => {
    try { await creativeApi.choose(it.id); toast('已设为 A 版（首图）', 'ok'); onChanged() } catch (e) { toast(errText(e), 'error') }
  }
  return (
    <Drawer title={<span className="inline">{slotName(it.slot)}{it.variant ? ` ${it.variant} 版` : ''}{it.seq > 1 ? ` · 第 ${it.seq} 张` : ''}{old && <span className="tag tag-gray">看的是第 {old.attempt} 次</span>}</span>} width={760} onClose={onClose}
      foot={!old && can('creative.review') && !it.human && !it.superseded ? <div className="grow"><JudgeBar it={it} onDone={() => { onChanged(); onClose() }} hotkeys /></div> : undefined}>
      <div className="stack" style={{ gap: 16 }}>
        <CompareView it={show} />
        <ReviewDetail it={show} />
        <TextRows it={show} canEdit={!old && can('creative.run')} onDone={onChanged} />
        {show.human && <div className="fs-12 t3">人工判断：{show.human === 'pass' ? '通过' : show.human === 'redraw' ? '要求重画' : '弃用'}{show.human_note ? ` · ${show.human_note}` : ''}</div>}
        {!old && can('creative.run') && (
          <div className="stack" style={{ gap: 6 }}>
            <div className="sub-title">单张重画</div>
            <div className="row" style={{ gap: 6 }}>
              <input className="input grow" placeholder="补一句要求，比如：耳钉再小一点、光从左边来" value={note} onChange={e => setNote(e.target.value)} />
              <button className="btn btn-secondary" onClick={redraw}><RefreshCw />重画这张</button>
            </div>
            <span className="fs-12 t4">重画占这套的生成次数，不占自动重画次数。</span>
          </div>
        )}
        {!old && it.slot === 'main1' && it.variant === 'B' && can('creative.review') && <button className="btn btn-ghost btn-sm" style={{ alignSelf: 'flex-start' }} onClick={chooseA}><Star />把这张设为首图 A 版</button>}
        {history.length > 0 && (
          <div className="stack" style={{ gap: 6 }}>
            <div className="sub-title inline"><History size={14} />之前的版本</div>
            <div className="cr-grid sm">
              {old && <ItemCard it={it} onOpen={() => setOld(null)} />}
              {history.sort((a, b) => b.attempt - a.attempt).map(h => <ItemCard key={h.id} it={h} active={old?.id === h.id} onOpen={() => setOld(h)} />)}
            </div>
          </div>
        )}
      </div>
    </Drawer>
  )
}

function CreateDrawer({ listingId, product, onClose, onCreated }: { listingId: number; product: Product; onClose: () => void; onCreated: (s: CreativeSet) => void }) {
  const toast = useToast()
  const [parts, setParts] = useState({ main: true, detail: true })
  const [p, setP] = useState<SetParams>({ hero_style: 'vertical', hero_b: true, sizes: [], note: '' })
  const [busy, setBusy] = useState(false)
  // 预填尺寸：info.size（如「8–9」「8-9mm」）
  useEffect(() => {
    const s = String(product.info?.size || '').trim()
    if (!s) return
    const m = s.match(/(\d+(?:\.\d+)?)(?:\s*[–\-~]\s*(\d+(?:\.\d+)?))?/)
    if (m) setP(v => ({ ...v, sizes: [{ label: s.replace(/\s*mm$/i, '') + ' mm', mm: m[2] ? (Number(m[1]) + Number(m[2])) / 2 : Number(m[1]) }] }))
  }, [product.info])
  const sizes = p.sizes || []
  const setSize = (i: number, k: 'label' | 'mm', v: string) => setP({ ...p, sizes: sizes.map((s, j) => j === i ? { ...s, [k]: k === 'mm' ? Number(v) || 0 : v } : s) })
  const submit = async () => {
    const list = Object.entries(parts).filter(([, v]) => v).map(([k]) => k)
    if (!list.length) return toast('至少选主图或详情页', 'error')
    if (sizes.some(s => !s.label.trim() || !(s.mm > 0))) return toast('尺寸要写名称和毫米数', 'error')
    setBusy(true)
    try { const r = await creativeApi.create(listingId, { parts: list, params: { ...p, note: p.note?.trim() } }); toast('已开始生成，一套大约 10–20 分钟', 'ok'); onCreated(r.set) }
    catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  return (
    <Drawer title="AI 出图" width={460} onClose={onClose} foot={<><button className="btn btn-secondary" onClick={onClose}>取消</button><button className="btn btn-primary" disabled={busy} onClick={submit}><Sparkles />开始生成</button></>}>
      <div className="stack">
        <Field label="出哪些">
          <div className="checks">
            <label><input type="checkbox" className="check" checked={parts.main} onChange={e => setParts({ ...parts, main: e.target.checked })} />主图 5 张 + SKU 图</label>
            <label><input type="checkbox" className="check" checked={parts.detail} onChange={e => setParts({ ...parts, detail: e.target.checked })} />详情页 9 屏</label>
          </div>
        </Field>
        {parts.main && <label className="row fs-13"><input type="checkbox" className="check" checked={!!p.hero_b} onChange={e => setP({ ...p, hero_b: e.target.checked })} />首图多出一张 B 版（换角度，留着做点击率对比）</label>}
        {parts.detail && (
          <Field label="首屏风格">
            <div className="segmented">{Object.entries(HERO_STYLE).map(([k, v]) => <button key={k} type="button" className={p.hero_style === k ? 'active' : ''} onClick={() => setP({ ...p, hero_style: k as SetParams['hero_style'] })}>{v}</button>)}</div>
          </Field>
        )}
        <Field label="尺寸" hint="写 2 个以上尺寸时，主图 4 出同一位模特分别戴每个尺寸的对比图；只有 1 个就用刻度尺实拍">
          <div className="stack" style={{ gap: 6 }}>
            {sizes.map((s, i) => (
              <div key={i} className="row" style={{ gap: 6 }}>
                <input className="input grow" placeholder="名称，如 8–9 mm" value={s.label} onChange={e => setSize(i, 'label', e.target.value)} />
                <input className="input mono" style={{ width: 90 }} inputMode="decimal" placeholder="毫米" value={s.mm || ''} onChange={e => setSize(i, 'mm', e.target.value)} />
                <button className="btn btn-ghost btn-icon btn-sm" aria-label="删除" onClick={() => setP({ ...p, sizes: sizes.filter((_, j) => j !== i) })}><Trash2 /></button>
              </div>
            ))}
            <button className="btn btn-ghost btn-sm" style={{ alignSelf: 'flex-start' }} onClick={() => setP({ ...p, sizes: [...sizes, { label: '', mm: 0 }] })}><Plus />加一个尺寸</button>
          </div>
        </Field>
        <Field label="补充要求" hint="会拼进每张图的提示词"><textarea className="input" rows={3} value={p.note} onChange={e => setP({ ...p, note: e.target.value })} placeholder="比如：这款偏冷白，别拍暖了" /></Field>
        <div className="fs-12 t3">每套最多生成 40 次（含重画），到了会暂停等你确认。首图、多尺寸对比图、校准期的模特图都要人工审。</div>
      </div>
    </Drawer>
  )
}
