import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { Save, FlaskConical, Plus, Trash2, Image as ImageIcon, Lock, X, ChevronUp, Store } from 'lucide-react'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { useListingRefs } from '../lib/listings'
import { useCategories, useCodeName } from '../lib/categories'
import { Topbar, Pill, Loading, Empty, Field } from './ui'
import { CompareView, ReviewDetail } from './CreativeShared'
import { MODE, TEMPLATE_GROUPS, creativeApi, slotName, templatePatch, type CreativeItem, type CreativeTemplate, type ProductMode, type ShopCreative } from '../lib/creative'
import '../styles/creative.css'

const SIZES = ['1024x1536', '1024x1024', '1536x1024']
// 改了这几项版本号会 +1（后端算），提醒一下
const VERSIONED: (keyof CreativeTemplate)[] = ['prompt', 'texts', 'product_mode', 'product_box', 'gen_size', 'lock_shape', 'size_compare', 'hero_style']

// 模板库的「生图模板」：每个位置一张图怎么生成。上面是给人看的（用途、用哪类照片、哪些店铺在用），提示词和画面参数收在「高级」里；可以挑一款试跑一张
export function CreativeTemplates({ switcher }: { switcher: ReactNode }) {
  const { can } = useAuth()
  const toast = useToast()
  const { items: cats } = useCategories()
  const refs = useListingRefs()
  const [list, setList] = useState<CreativeTemplate[] | null>(null)
  const [cat, setCat] = useState('')
  const [sel, setSel] = useState('')
  const [draft, setDraft] = useState<CreativeTemplate | null>(null)
  const [busy, setBusy] = useState(false)
  const [listingId, setListingId] = useState(0)
  const [trial, setTrial] = useState<CreativeItem | null>(null)
  const edit = can('creative.template')
  const load = useCallback(() => creativeApi.templates({ category: cat || undefined }).then(r => {
    setList(r.items || [])
    setSel(s => (r.items || []).some(t => t.key === s) ? s : r.items?.[0]?.key || '')
  }).catch(e => { toast(errText(e), 'error'); setList([]) }), [cat, toast])
  useEffect(() => { load() }, [load])
  const t = list?.find(x => x.key === sel)
  useEffect(() => { setDraft(t ? structuredClone(t) : null); setTrial(null) }, [t])
  const dirty = useMemo(() => !!t && !!draft && JSON.stringify(t) !== JSON.stringify(draft), [t, draft])
  const bump = !!t && !!draft && VERSIONED.some(k => JSON.stringify(t[k]) !== JSON.stringify(draft[k]))
  const shopRefs = (refs || []).filter(r => !cat || r.product.category?.key === cat)
  useEffect(() => { setListingId(id => shopRefs.some(r => r.id === id) ? id : shopRefs[0]?.id || 0) }, [refs, cat]) // eslint-disable-line react-hooks/exhaustive-deps

  const save = async () => {
    if (!draft || !t) return
    setBusy(true)
    try {
      await creativeApi.saveTemplate(draft.key, templatePatch(t, draft))
      toast(bump ? '已保存，版本号加 1，之后生成的图用新版本' : '已保存', 'ok'); load()
    } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const runTrial = async () => {
    if (!t || !listingId) return
    setBusy(true)
    try {
      const r = await creativeApi.trial(t.key, listingId)
      if (r.item) setTrial(r.item)
      toast(r.item?.output_url ? '试跑完成' : '已开始试跑，结果在后台任务里，完成后刷新这里', 'ok')
    } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const set = <K extends keyof CreativeTemplate>(k: K, v: CreativeTemplate[K]) => setDraft(d => d ? { ...d, [k]: v } : d)
  // 加照片类型的下拉：按当前品类（没选就合并所有品类）
  const codeOptions = useMemo(() => {
    const seen = new Set<string>()
    return (cats || []).filter(c => !cat || c.key === cat).flatMap(c => c.codes || []).filter(c => !seen.has(c.code) && !!seen.add(c.code)).map(c => ({ code: c.code, name: c.name, group: c.group.replace(/^[A-Z]\s*/, '') }))
  }, [cats, cat])
  const groups: [string, CreativeTemplate[]][] = TEMPLATE_GROUPS.map(g => [g.label, (list || []).filter(x => g.slots.includes(x.slot) || (g.key === 'main' && x.kind === 'main' && !TEMPLATE_GROUPS[0].slots.includes(x.slot)))])
  const codeName = useCodeName()
  const catId = cats?.find(c => c.key === cat)?.id
  const box = draft?.product_box || [0, 0, 0, 0]

  return (
    <>
      <Topbar crumbs={['模板库', '生图模板', ...(t ? [t.name] : [])]}>
        {switcher}
        {edit && draft && <button className="btn btn-primary" disabled={!dirty || busy} onClick={save}><Save />保存</button>}
      </Topbar>
      <div className="lib">
        <aside className="sidebar lib-filters">
          {(cats?.length || 0) > 1 && (
            <div className="nav-group">
              <div className="nav-label">品类</div>
              <select className="input" value={cat} onChange={e => setCat(e.target.value)}><option value="">全部品类</option>{cats!.map(c => <option key={c.id} value={c.key}>{c.name}</option>)}</select>
            </div>
          )}
          {!list ? <div className="t3 fs-12" style={{ padding: 8 }}>加载中…</div> : groups.map(([label, items]) => (
            <div key={label} className="nav-group">
              <div className="nav-label">{label}<span className="end">{items.length}</span></div>
              {items.map(x => (
                <a key={x.key} className={'nav-item tpl-item' + (x.key === sel ? ' active' : '') + (x.enabled ? '' : ' off')} onClick={() => setSel(x.key)} title={x.purpose}>
                  <span className="tpl-slot">{slotName(x.slot)}</span><span className="ellipsis">{x.name}</span>
                  <span className="end inline">{!x.enabled && <span className="tag tag-gray">停用</span>}{x.source === 'custom' && <span className="tag tag-blue">改过</span>}</span>
                </a>
              ))}
            </div>
          ))}
        </aside>

        <div className="lib-main"><div className="scroll"><main className="content" style={{ maxWidth: 880 }}>
          {!list ? <Loading /> : !draft ? <Empty title="没有生图模板" desc="模板文件在 templates/生图模板，服务启动时自动导入。" /> : (
            <>
              <section className="panel">
                <div className="panel-head">
                  <div className="titles"><div className="panel-title inline">{draft.name}<span className="t4 fs-12">第 {t?.version} 版</span>{bump && <span className="tag tag-orange">保存后第 {(t?.version || 0) + 1} 版</span>}</div><div className="panel-sub">{draft.kind === 'main' ? '主图' : '详情页'} · {slotName(draft.slot)} · 适用 {draft.categories.length ? draft.categories.map(k => cats?.find(c => c.key === k)?.name || k).join('、') : '全部品类'}</div></div>
                  <label className="row fs-13"><input type="checkbox" className="check" disabled={!edit} checked={draft.enabled} onChange={e => set('enabled', e.target.checked)} />启用</label>
                </div>
                <div className="panel-pad stack">
                  <div className="form-row">
                    <Field label="名称"><input className="input" disabled={!edit} value={draft.name} onChange={e => set('name', e.target.value)} /></Field>
                    <Field label="这张图做什么"><input className="input" disabled={!edit} value={draft.purpose} onChange={e => set('purpose', e.target.value)} /></Field>
                  </div>
                  <div className="form-row">
                    <Field label="商品怎么放进图里">
                      <select className="input" disabled={!edit} value={draft.product_mode} onChange={e => set('product_mode', e.target.value as ProductMode)}>{Object.entries(MODE).map(([k, v]) => <option key={k} value={k}>{v}</option>)}</select>
                    </Field>
                    <Field label="这一屏出几张"><input className="input mono" type="number" min={1} max={3} disabled={!edit} value={draft.count} onChange={e => set('count', Math.min(3, Math.max(1, Number(e.target.value) || 1)))} /></Field>
                  </div>
                  <div className="form-field">
                    <span className="label">用哪类实拍照片<span className="help">排前面的优先；只取实拍和只做过高清化的</span></span>
                    <div className="inline">
                      {draft.photo_codes.map((c, i) => <span key={c} className="tag tpl-code" title={`拍摄编号 ${c}`}>
                        <span className="t4 mono">{i + 1}</span>{codeName(c, catId)}
                        {edit && i > 0 && <button type="button" className="tag-x" title="往前挪" aria-label="往前挪" onClick={() => { const a = [...draft.photo_codes]; [a[i - 1], a[i]] = [a[i], a[i - 1]]; set('photo_codes', a) }}><ChevronUp /></button>}
                        {edit && <button type="button" className="tag-x" title="去掉" aria-label="去掉" onClick={() => set('photo_codes', draft.photo_codes.filter(x => x !== c))}><X /></button>}
                      </span>)}
                      {!draft.photo_codes.length && <span className="t4 fs-12">{draft.product_mode === 'none' ? '这张图不放商品' : '还没选'}</span>}
                      {edit && <select className="input input-sm" style={{ width: 160 }} value="" onChange={e => e.target.value && set('photo_codes', [...draft.photo_codes, e.target.value])}>
                        <option value="">＋ 加一类照片</option>
                        {codeOptions.filter(o => !draft.photo_codes.includes(o.code)).map(o => <option key={o.code} value={o.code}>{o.group} · {o.name}</option>)}
                      </select>}
                    </div>
                  </div>
                  <details className="tpl-advanced">
                    <summary>高级：提示词和画面参数</summary>
                    <div className="stack" style={{ marginTop: 10 }}>
                  <Field label="提示词（给 AI 的画面描述，英文）" hint="占位：{场景} 静物背景台面 · {穿搭} 上身穿搭（都来自场景库）· {款名} {英文名} {一句话} {商品} {尺寸} {颜色} {品类} {文字} {模特} {佩戴位置}。通用要求由 prompts/生图-主图.md、生图-详情.md 拼在前面，这里只写这张图的画面">
                    <textarea className="input cr-prompt" disabled={!edit} rows={7} value={draft.prompt} onChange={e => set('prompt', e.target.value)} placeholder={draft.product_mode === 'photo' ? '直接用实拍时不调生图，不用写' : ''} />
                  </Field>
                  <Field label="生成尺寸" hint="生图接口只认这三种">
                    <select className="input mono" disabled={!edit} value={draft.gen_size} onChange={e => set('gen_size', e.target.value)}>{SIZES.map(s => <option key={s}>{s}</option>)}</select>
                  </Field>
                  {draft.product_mode === 'pixel_lock' && (
                    <Field label="商品位置" hint="生成画布上的比例 [x, y, 宽, 高]；3:4 导出会上下各裁掉约 5%，1:1 导出裁掉约 17%，商品要放在安全区里">
                      <div className="row" style={{ gap: 12, alignItems: 'flex-start' }}>
                        <div className="cr-tpl-box"><span style={{ left: `${box[0] * 100}%`, top: `${box[1] * 100}%`, width: `${box[2] * 100}%`, height: `${box[3] * 100}%` }} /></div>
                        <div className="form-row grow">
                          {['x', 'y', '宽', '高'].map((l, i) => <Field key={l} label={l}><input className="input mono" inputMode="decimal" disabled={!edit} value={box[i]} onChange={e => { const b = [...box] as [number, number, number, number]; b[i] = Math.min(1, Math.max(0, Number(e.target.value) || 0)); set('product_box', b) }} /></Field>)}
                        </div>
                      </div>
                    </Field>
                  )}
                  {draft.product_mode === 'model' && draft.wear && (
                    <div className="fs-12 t3 inline"><span>模特位：{draft.wear.slot}</span><span className="sep" />部位：{draft.wear.body_part === 'auto' ? '按款式自动' : draft.wear.body_part}<span className="sep" />取景：{draft.wear.framing}</div>
                  )}
                    </div>
                  </details>
                </div>
              </section>

              <section className="panel">
                <div className="panel-head"><div className="titles"><div className="panel-title">图上的字</div><div className="panel-sub">字从文案里取（content.*、listing.*、info.*），或写 literal:原文。勾「参数」的（尺寸、材质、价格这类）逐字核对，不对不能导出</div></div>
                  {edit && <button className="btn btn-ghost btn-sm" onClick={() => set('texts', [...draft.texts, { key: '', from: '', max: 10, critical: false }])}><Plus />加一条</button>}
                </div>
                {draft.texts.length === 0 ? <div className="panel-pad t3 fs-12">这张图不写字。</div> : (
                  <div className="table-wrap"><table className="table">
                    <thead><tr><th style={{ width: 120 }}>名字</th><th>从哪取</th><th className="num" style={{ width: 80 }}>最多字数</th><th style={{ width: 70 }}>参数</th><th style={{ width: 40 }} /></tr></thead>
                    <tbody>{draft.texts.map((x, i) => {
                      const up = (v: Partial<typeof x>) => set('texts', draft.texts.map((y, j) => j === i ? { ...y, ...v } : y))
                      return (
                        <tr key={i}>
                          <td><input className="input input-sm mono" disabled={!edit} value={x.key} onChange={e => up({ key: e.target.value })} /></td>
                          <td><input className="input input-sm mono" disabled={!edit} value={x.from} onChange={e => up({ from: e.target.value })} /></td>
                          <td className="num"><input className="input input-sm mono" style={{ width: 64 }} disabled={!edit} value={x.max} onChange={e => up({ max: Number(e.target.value) || 0 })} /></td>
                          <td><input type="checkbox" className="check" disabled={!edit} checked={x.critical} onChange={e => up({ critical: e.target.checked })} /></td>
                          <td>{edit && <button className="btn btn-ghost btn-icon btn-sm" aria-label="删除" onClick={() => set('texts', draft.texts.filter((_, j) => j !== i))}><Trash2 /></button>}</td>
                        </tr>
                      )
                    })}</tbody>
                  </table></div>
                )}
              </section>

              {trial && (
                <section className="panel">
                  <div className="panel-head"><div className="panel-title">试跑结果</div><span className="t4 fs-12">不进任何一套，只用来调模板</span></div>
                  <div className="panel-pad stack"><CompareView it={trial} /><ReviewDetail it={trial} /></div>
                </section>
              )}
            </>
          )}
        </main></div></div>

        <aside className="inspector lib-inspector">
          {t && <div className="inspector-body">
            <ShopUse template={t} />
            <div className="inspector-section">
              <div className="head"><span className="section-title">导出</span></div>
              <div className="inline fs-12">{t.export.map(e => <span key={e.name} className="tag tag-gray mono">{e.w}×{e.h || '自适应'}</span>)}</div>
              <div className="fs-12 t3 inline">{t.product_mode === 'pixel_lock' ? <><Lock size={12} />商品像素原样贴回，导出时从原图再贴一次</> : t.product_mode === 'photo' ? <><ImageIcon size={12} />直接用实拍，只裁切和高清化</> : MODE[t.product_mode]}</div>
              {t.style_refs.length > 0 ? <div className="fs-12 t3">风格参考 {t.style_refs.length} 张</div> : <div className="fs-12 t4">没有风格参考图</div>}
            </div>
            {edit && (
              <div className="inspector-section">
                <div className="head"><span className="sub-title">试跑一张</span></div>
                <select className="input" value={listingId} onChange={e => setListingId(Number(e.target.value))}>
                  {shopRefs.length === 0 && <option value={0}>没有可用的款</option>}
                  {shopRefs.map(r => <option key={r.id} value={r.id}>{r.product.name}</option>)}
                </select>
                <button className="btn btn-secondary btn-block" disabled={busy || !listingId || dirty || t.product_mode === 'photo'} onClick={runTrial}><FlaskConical />用这款试跑</button>
                <span className="fs-12 t4">{dirty ? '先保存再试跑。' : '会真实调用生图和复核，消耗额度。'}</span>
              </div>
            )}
            <div className="inspector-section">
              <div className="head"><span className="sub-title">状态</span></div>
              <div className="inline">{t.enabled ? <Pill kind="pill-success">启用</Pill> : <Pill kind="pill-neutral">停用</Pill>}{t.source === 'custom' ? <Pill kind="pill-info">在页面上改过</Pill> : <Pill kind="pill-neutral">来自模板文件</Pill>}</div>
              <span className="fs-12 t4">模板文件升级时，没在页面上改过的会自动换成新版；改过的不覆盖。</span>
            </div>
          </div>}
        </aside>
      </div>
    </>
  )
}

// 这个位置每个店铺用哪张模板（店铺出图设置 template_set）；一个位置有几张备选时在这里切
function ShopUse({ template }: { template: CreativeTemplate }) {
  const { me, can } = useAuth()
  const toast = useToast()
  const shops = (me?.shops || []).filter(s => !s.archived)
  const [cfg, setCfg] = useState<Record<number, ShopCreative>>({})
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    if (!can('creative.template')) return
    Promise.all(shops.map(s => creativeApi.shop(s.id).then(r => [s.id, r] as const).catch(() => null)))
      .then(rs => setCfg(Object.fromEntries(rs.filter(Boolean) as [number, ShopCreative][])))
  }, [shops.length]) // eslint-disable-line react-hooks/exhaustive-deps
  if (!can('creative.template') || !shops.length || template.slot === 'd1') return null
  const choose = async (shopId: number, on: boolean) => {
    const cur = cfg[shopId]
    if (!cur) return
    const next = { ...cur, template_set: { ...cur.template_set } }
    if (on) next.template_set[template.slot] = template.key
    else delete next.template_set[template.slot]
    setBusy(true)
    try { await creativeApi.saveShop(shopId, next); setCfg(c => ({ ...c, [shopId]: next })); toast(on ? '已设为这个店铺的模板，之后新出的套装生效' : '已改回默认模板', 'ok') }
    catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  return (
    <div className="inspector-section">
      <div className="head"><span className="section-title">哪些店铺在用</span></div>
      {shops.map(s => {
        const picked = cfg[s.id]?.template_set[template.slot]
        const on = picked === template.key
        return <div key={s.id} className="row between fs-13">
          <span className="inline"><Store size={14} />{s.name}</span>
          {on ? <button className="btn btn-ghost btn-sm" disabled={busy} onClick={() => choose(s.id, false)} title="改回默认">在用 · 取消</button>
            : <button className="btn btn-secondary btn-sm" disabled={busy || !cfg[s.id] || !template.enabled} onClick={() => choose(s.id, true)}>{picked ? '改用这张' : '用这张'}</button>}
        </div>
      })}
      <span className="fs-12 t4">没选的店铺用这个位置的默认模板。首屏按出图时选的版式来。</span>
    </div>
  )
}
