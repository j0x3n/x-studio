import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { Save, Unlock, Lock, Check, X } from 'lucide-react'
import { api, qs, type Page } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import type { ShopModel } from '../lib/tryon'
import { Topbar, Pill, Loading, Empty, Field } from './ui'
import { DETAIL_SLOTS, MAIN_SLOTS, MODEL_SLOT, REASON, WEAR, creativeApi, mockOn, slotName, type CreativeSettings as CS, type CreativeTemplate, type ShopCreative, type Stats } from '../lib/creative'

const DEFAULTS: CS = { final_glance: true, max_images_per_listing: 40, auto_pass_score: 90, manual_score_min: 60, max_auto_redraw: 2, calibration_count: 20, calibration_agreement: 0.95, spot_check_rate: 0.1, size_tolerance: 0.2, size_compare_tolerance: 0.1, return_reason_threshold: 2 }
// 比例类字段在页面上按百分数填
const FIELDS: { k: keyof CS; label: string; hint: string; pct?: boolean }[] = [
  { k: 'max_images_per_listing', label: '每套生成上限（次）', hint: '含重画、改字；到了暂停，要人点确认' },
  { k: 'auto_pass_score', label: '自动通过分数', hint: '两次复核总分都不低于它' },
  { k: 'manual_score_min', label: '自动重画分数线', hint: '两次都低于它且还能重画时自动重画' },
  { k: 'max_auto_redraw', label: '每个位置自动重画次数', hint: '用完还不过就转人工' },
  { k: 'calibration_count', label: '校准期张数', hint: '每类模特图前多少张全部人工审' },
  { k: 'calibration_agreement', label: '开放自动通过的一致率', hint: '人和 AI 判断一致的比例', pct: true },
  { k: 'spot_check_rate', label: '抽检比例', hint: '本该自动通过的图随机转人工', pct: true },
  { k: 'size_tolerance', label: '尺寸偏差上限', hint: 'AI 估算和资料 mm 比', pct: true },
  { k: 'size_compare_tolerance', label: '多尺寸对比偏差上限', hint: '各张之间的大小比例和 mm 比例', pct: true },
  { k: 'return_reason_threshold', label: '售后回流触发次数', hint: '同款同一退货原因累计几次，就出改图建议并收紧这一类' }
]

export function CreativeSettingsTab({ tabs }: { tabs: ReactNode }) {
  const toast = useToast()
  const [s, setS] = useState<CS | null>(null)
  const [vals, setVals] = useState<Record<string, string>>({})
  const [glance, setGlance] = useState(true)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    const done = (c: Partial<CS>) => {
      const v = { ...DEFAULTS, ...c }
      setS(v); setGlance(v.final_glance)
      setVals(Object.fromEntries(FIELDS.map(f => [f.k, String(f.pct ? Math.round((v[f.k] as number) * 1000) / 10 : v[f.k])])))
    }
    if (mockOn()) { done({}); return }
    api<{ creative?: Partial<CS> }>('/api/settings').then(r => done(r.creative || {})).catch(e => toast(errText(e), 'error'))
  }, [toast])
  const save = async () => {
    const body: Record<string, unknown> = { final_glance: glance }
    for (const f of FIELDS) {
      const n = Number(vals[f.k])
      if (vals[f.k].trim() === '' || !Number.isFinite(n) || n < 0) return toast(`「${f.label}」要填非负数`, 'error')
      body[f.k] = f.pct ? n / 100 : Math.round(n)
    }
    setBusy(true)
    try { await api('/api/settings', { method: 'PATCH', body: { creative: body } }); toast('复核阈值已保存', 'ok') } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  return (
    <>
      <Topbar title="设置" sub="AI 出图：每个店用哪位模特、默认模板，以及复核和分流的阈值"><button className="btn btn-primary" disabled={!s || busy} onClick={save}><Save />保存阈值</button></Topbar>
      <div className="scroll"><main className="content" style={{ maxWidth: 1080 }}>
        {tabs}
        <ShopCreativePanel />
        {!s ? <Loading /> : (
          <section className="panel">
            <div className="panel-head"><div className="titles"><div className="panel-title">复核和分流</div><div className="panel-sub">首图、多尺寸对比图永远要人工审；这里调的是其他图什么时候能自动通过</div></div></div>
            <div className="panel-pad stack">
              <label className="row fs-13"><input type="checkbox" className="check" checked={glance} onChange={e => setGlance(e.target.checked)} />导出前必须有人点「整套过目」（上线第一个月建议开着）</label>
              <div className="form-row">
                {FIELDS.map(f => <Field key={f.k} label={f.label + (f.pct ? '（%）' : '')} hint={f.hint}><input className="input mono" inputMode="decimal" value={vals[f.k] ?? ''} onChange={e => setVals({ ...vals, [f.k]: e.target.value })} /></Field>)}
              </div>
            </div>
          </section>
        )}
        <StatsPanel />
      </main></div>
    </>
  )
}

function ShopCreativePanel() {
  const { me, can } = useAuth()
  const toast = useToast()
  const shops = me?.shops || []
  const [shopId, setShopId] = useState(shops[0]?.id || 0)
  const [cfg, setCfg] = useState<ShopCreative | null>(null)
  const [models, setModels] = useState<ShopModel[]>([])
  const [tpls, setTpls] = useState<CreativeTemplate[]>([])
  const [busy, setBusy] = useState(false)
  const edit = can('creative.template')
  useEffect(() => { creativeApi.templates({}).then(r => setTpls(r.items || [])).catch(() => setTpls([])) }, [])
  useEffect(() => {
    if (!shopId) return
    setCfg(null)
    creativeApi.shop(shopId).then(setCfg).catch(e => toast(errText(e), 'error'))
    if (mockOn()) setModels([])
    else api<Page<ShopModel>>('/api/models' + qs({ shop_id: shopId, page_size: 100 })).then(r => setModels(r.items || [])).catch(() => setModels([]))
  }, [shopId, toast])
  if (!shops.length) return null
  const save = async () => {
    if (!cfg) return
    setBusy(true)
    try { await creativeApi.saveShop(shopId, cfg); toast('店铺出图设置已保存', 'ok') } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const slotTpls = (slot: string) => tpls.filter(t => t.slot === slot && t.enabled)
  return (
    <section className="panel">
      <div className="panel-head">
        <div className="titles"><div className="panel-title">店铺 AI 出图</div><div className="panel-sub">模特图用店铺固定模特；没选模特时，模特类的位置会报错让你先来这里选</div></div>
        {shops.length > 1 && <select className="input" style={{ width: 160 }} value={shopId} onChange={e => setShopId(Number(e.target.value))}>{shops.map(x => <option key={x.id} value={x.id}>{x.name}</option>)}</select>}
        {edit && <button className="btn btn-secondary btn-sm" disabled={!cfg || busy} onClick={save}><Save />保存</button>}
      </div>
      {!cfg ? <Loading /> : (
        <div className="panel-pad stack">
          <div className="form-row">
            {(Object.keys(MODEL_SLOT) as (keyof typeof MODEL_SLOT)[]).map(k => (
              <Field key={k} label={MODEL_SLOT[k]}>
                <select className="input" disabled={!edit} value={cfg.models[k] || 0} onChange={e => setCfg({ ...cfg, models: { ...cfg.models, [k]: Number(e.target.value) || null } })}>
                  <option value={0}>不用</option>
                  {models.filter(m => !m.archived).map(m => <option key={m.id} value={m.id}>{m.name}</option>)}
                </select>
              </Field>
            ))}
          </div>
          {models.length === 0 && <div className="fs-12 t3">这个店的模特库还是空的，先去「模特库」建 F1、M1 两位。</div>}
          <div className="sub-title">每个位置默认用哪个模板</div>
          <div className="form-row">
            {[...MAIN_SLOTS, ...DETAIL_SLOTS].filter(sl => slotTpls(sl).length > 1).map(sl => (
              <Field key={sl} label={`${sl.startsWith('d') ? '详情' : '主图'} · ${slotName(sl)}`}>
                <select className="input" disabled={!edit} value={cfg.template_set[sl] || ''} onChange={e => setCfg({ ...cfg, template_set: { ...cfg.template_set, [sl]: e.target.value } })}>
                  <option value="">按规则自动选</option>
                  {slotTpls(sl).map(t => <option key={t.key} value={t.key}>{t.name}</option>)}
                </select>
              </Field>
            ))}
          </div>
          <div className="fs-12 t4">只有一个模板的位置不用选。首屏风格在每次出图时选；尺寸位按尺寸个数自动选模特对比或刻度尺。</div>
        </div>
      )}
    </section>
  )
}

const catName = (c: string) => c.startsWith('tight:') ? `${slotName(c.slice(6))}（售后收紧）` : WEAR[c] || c
const pct = (n?: number) => typeof n === 'number' ? `${Math.round(n * 1000) / 10}%` : '—'

function StatsPanel() {
  const { can } = useAuth()
  const toast = useToast()
  const [st, setSt] = useState<Stats | null>(null)
  const load = useCallback(() => creativeApi.stats().then(setSt).catch(() => setSt({ categories: [], suggestions: [] })), [])
  useEffect(() => { load() }, [load])
  const tpl = can('creative.template'), review = can('creative.review')
  const act = async (f: () => Promise<unknown>) => { try { await f(); load() } catch (e) { toast(errText(e), 'error') } }
  if (!review) return null
  return (
    <>
      <section className="panel">
        <div className="panel-head"><div className="titles"><div className="panel-title">校准和一致率</div><div className="panel-sub">每类模特图人工审满校准张数、且人和 AI 一致率达标后，才开放自动通过</div></div></div>
        {!st ? <Loading /> : !st.categories.length ? <div className="panel-pad t3 fs-12">还没有人工判断记录。</div> : (
          <div className="table-wrap"><table className="table">
            <thead><tr><th>类别</th><th className="num">已审</th><th className="num">一致</th><th className="num">一致率</th><th className="num">自动通过率</th><th className="num">人工改判率</th><th className="num">关联退货</th><th>状态</th><th /></tr></thead>
            <tbody>{st.categories.map(c => (
              <tr key={c.category}>
                <td>{catName(c.category)}</td>
                <td className="num mono">{c.total}</td><td className="num mono">{c.agree}</td>
                <td className="num mono">{c.total ? pct(c.agree / c.total) : '—'}</td>
                <td className="num mono">{pct(c.auto_pass_rate)}</td><td className="num mono">{pct(c.override_rate)}</td><td className="num mono">{c.return_links ?? 0}</td>
                <td>{c.tightened ? <Pill kind="pill-warning">已收紧</Pill> : c.open ? <Pill kind="pill-success">可自动通过</Pill> : <Pill kind="pill-neutral">校准中</Pill>}{c.tightened_reason && <div className="cell-sub">{c.tightened_reason}</div>}</td>
                <td className="num">{tpl && (c.tightened
                  ? <button className="btn btn-ghost btn-sm" onClick={() => act(() => creativeApi.calibration(c.category, { tightened: false }))}><Unlock />解除收紧</button>
                  : c.open && <button className="btn btn-ghost btn-sm" onClick={() => act(() => creativeApi.calibration(c.category, { open: false }))}><Lock />关闭自动通过</button>)}</td>
              </tr>
            ))}</tbody>
          </table></div>
        )}
      </section>
      <section className="panel">
        <div className="panel-head"><div className="titles"><div className="panel-title">售后回流的改图建议</div><div className="panel-sub">同一款同一个退货原因累计到触发次数，就建议改对应位置的图</div></div></div>
        {!st ? <Loading /> : !st.suggestions.filter(x => x.status === 'open').length ? <Empty title="没有待处理的改图建议" /> : (
          <div className="list">{st.suggestions.filter(x => x.status === 'open').map(x => (
            <div key={x.id} className="list-item">
              <span className="grow stack" style={{ gap: 2 }}>
                <span className="cell-title">{x.product_name || `上架版本 #${x.listing_id}`} · 改{slotName(x.slot)}</span>
                <span className="fs-12 t3">「{REASON[x.reason_code] || x.reason_code}」退货 {x.count} 次</span>
              </span>
              <button className="btn btn-secondary btn-sm" onClick={() => act(() => creativeApi.suggestion(x.id, 'done'))}><Check />已改</button>
              <button className="btn btn-ghost btn-sm" onClick={() => act(() => creativeApi.suggestion(x.id, 'ignored'))}><X />忽略</button>
            </div>
          ))}</div>
        )}
      </section>
    </>
  )
}

// 数据与运营的设置：每日提醒、标题改动间隔、付费门槛、新品期；店铺日预算在「数据」页里改
type MarketCfg = { remind_enabled: boolean; remind_time: string; title_change_days: number; min_paid_price: number; new_listing_days: number }
const MARKET_DEFAULTS: MarketCfg = { remind_enabled: true, remind_time: '10:00', title_change_days: 7, min_paid_price: 99, new_listing_days: 14 }
export function MarketSettingsTab({ tabs }: { tabs: ReactNode }) {
  const toast = useToast()
  const [m, setM] = useState<MarketCfg | null>(null)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    if (mockOn()) { setM(MARKET_DEFAULTS); return }
    api<{ market?: Partial<MarketCfg> }>('/api/settings').then(r => setM({ ...MARKET_DEFAULTS, ...(r.market || {}) })).catch(e => toast(errText(e), 'error'))
  }, [toast])
  const save = async () => {
    if (!m) return
    if (!/^([01]\d|2[0-3]):[0-5]\d$/.test(m.remind_time)) return toast('提醒时间写成 10:00 这样', 'error')
    setBusy(true)
    try { await api('/api/settings', { method: 'PATCH', body: { market: m } }); toast('已保存', 'ok') } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const num = (k: keyof MarketCfg) => (e: React.ChangeEvent<HTMLInputElement>) => m && setM({ ...m, [k]: Math.max(0, Math.floor(Number(e.target.value) || 0)) })
  return (
    <>
      <Topbar title="设置" sub="数据与运营：每天提醒导数据、诊断规则的门槛"><button className="btn btn-primary" disabled={!m || busy} onClick={save}><Save />保存</button></Topbar>
      <div className="scroll"><main className="content" style={{ maxWidth: 880 }}>
        {tabs}
        {!m ? <Loading /> : (
          <section className="panel">
            <div className="panel-head"><div className="titles"><div className="panel-title">数据与运营</div><div className="panel-sub">服务器不登录淘宝；到点提醒你在已登录的浏览器里用采集助手点一下</div></div></div>
            <div className="panel-pad stack">
              <label className="row fs-13"><input type="checkbox" className="check" checked={m.remind_enabled} onChange={e => setM({ ...m, remind_enabled: e.target.checked })} />每天提醒导入昨天的数据</label>
              <div className="form-row">
                <Field label="提醒时间（北京时间）"><input className="input mono" value={m.remind_time} onChange={e => setM({ ...m, remind_time: e.target.value })} /></Field>
                <Field label="同一款改标题间隔（天）" hint="改得太勤看不出效果"><input className="input mono" inputMode="numeric" value={m.title_change_days} onChange={num('title_change_days')} /></Field>
                <Field label="付费推广最低售价（元）" hint="低于它的款建议只做自然搜索"><input className="input mono" inputMode="numeric" value={m.min_paid_price} onChange={num('min_paid_price')} /></Field>
                <Field label="新品期（天）" hint="上架这么多天内只投中长尾词"><input className="input mono" inputMode="numeric" value={m.new_listing_days} onChange={num('new_listing_days')} /></Field>
              </div>
            </div>
          </section>
        )}
      </main></div>
    </>
  )
}

