import { useEffect, useState } from 'react'
import { api, type Page } from '../lib/api'
import { creativeApi, DETAIL_SLOTS, MAIN_SLOTS, slotName, type CreativeTemplate } from '../lib/creative'
import { sceneApi, type Scene } from '../lib/scenes'
import { MODEL_SLOTS, type ModelSlotKey, type PlanConfig, type PlanOverrides } from '../lib/views'
import type { ShopModel } from '../lib/tryon'
import type { DetailTemplate } from '../lib/types'
import { Field } from './ui'

// 出图方案的选项：生图模板、模特、场景、详情版式。方案页和商品页共用一份
export type PlanOptions = { templates: CreativeTemplate[]; models: ShopModel[]; scenes: Scene[]; layouts: DetailTemplate[] }
let cache: Promise<PlanOptions> | null = null
export function usePlanOptions() {
  const [v, setV] = useState<PlanOptions | null>(null)
  useEffect(() => {
    cache ||= Promise.all([
      creativeApi.templates({}).then(r => r.items).catch(() => []),
      api<Page<ShopModel>>('/api/models?page_size=200').then(r => r.items || []).catch(() => []),
      sceneApi.list().catch(() => []),
      api<{ detail: DetailTemplate[] }>('/api/templates').then(r => r.detail || []).catch(() => [])
    ]).then(([templates, models, scenes, layouts]) => ({ templates, models, scenes, layouts }))
    cache.then(setV)
    return () => { cache = null } // 下次打开重新拉，改过模特、场景能看到
  }, [])
  return v
}

const SLOTS = [...MAIN_SLOTS, ...DETAIL_SLOTS]
const slotLabel = (s: string) => `${s.startsWith('d') ? '详情' : '主图'} · ${slotName(s)}`

// 方案本身：每个位置的模板、各模特位的模特（可多选）、场景、详情版式
export function PlanFields({ value, onChange, options, categories, disabled }: { value: PlanConfig; onChange: (v: PlanConfig) => void; options: PlanOptions; categories?: string[]; disabled?: boolean }) {
  const tpls = (slot: string) => options.templates.filter(t => t.slot === slot && t.enabled && (!categories?.length || !t.categories.length || t.categories.some(c => categories.includes(c))))
  return <div className="stack">
    <div className="sub-title">模特</div>
    <div className="plan-models">
      {(Object.keys(MODEL_SLOTS) as ModelSlotKey[]).map(k => <Field key={k} label={MODEL_SLOTS[k]}>
        <ModelPicker models={options.models} value={value.models[k] || []} disabled={disabled} onChange={ids => onChange({ ...value, models: { ...value.models, [k]: ids } })} />
      </Field>)}
    </div>
    <div className="form-row">
      <Field label="场景"><select className="input" disabled={disabled} value={value.scene_id || 0} onChange={e => onChange({ ...value, scene_id: Number(e.target.value) || null })}>
        <option value={0}>不指定（用店铺默认场景）</option>{options.scenes.map(s => <option key={s.id} value={s.id}>{s.name}</option>)}
      </select></Field>
      <Field label="详情页版式"><select className="input" disabled={disabled} value={value.detail_layout} onChange={e => onChange({ ...value, detail_layout: e.target.value })}>
        <option value="">店铺默认</option>{options.layouts.map(t => <option key={t.key} value={t.key}>{t.name}</option>)}
      </select></Field>
    </div>
    <div className="sub-title">每个位置用哪个模板</div>
    <div className="form-row plan-slots">
      {SLOTS.filter(s => tpls(s).length > 0).map(s => <Field key={s} label={slotLabel(s)}>
        <select className="input" disabled={disabled} value={value.template_set[s] || ''} onChange={e => { const ts = { ...value.template_set }; if (e.target.value) ts[s] = e.target.value; else delete ts[s]; onChange({ ...value, template_set: ts }) }}>
          <option value="">按店铺设置</option>{tpls(s).map(t => <option key={t.key} value={t.key}>{t.name}</option>)}
        </select>
      </Field>)}
    </div>
  </div>
}

function ModelPicker({ models, value, onChange, disabled }: { models: ShopModel[]; value: number[]; onChange: (ids: number[]) => void; disabled?: boolean }) {
  const live = models.filter(m => !m.archived || value.includes(m.id))
  if (!live.length) return <span className="t4 fs-12">模特库是空的</span>
  return <div className="plan-model-list">
    {live.map(m => <label key={m.id} className={'filter-opt' + (value.includes(m.id) ? ' on' : '')}>
      <input type="checkbox" className="check" disabled={disabled} checked={value.includes(m.id)} onChange={e => onChange(e.target.checked ? [...value, m.id] : value.filter(x => x !== m.id))} />
      {m.name}{m.shop_id === null && <span className="t4">全局</span>}
    </label>)}
  </div>
}

// 商品覆盖：每一项可以「沿用方案」或单独指定；模板和模特按位置覆盖，场景和版式整项覆盖
export function OverrideFields({ base, value, onChange, options, categories, disabled }: { base: PlanConfig; value: PlanOverrides; onChange: (v: PlanOverrides) => void; options: PlanOptions; categories?: string[]; disabled?: boolean }) {
  const tpls = (slot: string) => options.templates.filter(t => t.slot === slot && t.enabled && (!categories?.length || !t.categories.length || t.categories.some(c => categories.includes(c))))
  const tplName = (key?: string) => options.templates.find(t => t.key === key)?.name || key || '按店铺设置'
  const sceneName = (id: number | null) => id ? options.scenes.find(s => s.id === id)?.name || `场景 #${id}` : '店铺默认场景'
  const layoutName = (key: string) => key ? options.layouts.find(t => t.key === key)?.name || key : '店铺默认'
  const modelNames = (ids?: number[]) => ids?.length ? ids.map(id => options.models.find(m => m.id === id)?.name || `#${id}`).join('、') : '不用'
  const ts = value.template_set || {}
  const ms = value.models || {}
  const setTs = (slot: string, key: string | null) => { const next = { ...ts }; if (key === null) delete next[slot]; else next[slot] = key; onChange({ ...value, template_set: next }) }
  const setMs = (k: ModelSlotKey, ids: number[] | null) => { const next = { ...ms }; if (ids === null) delete next[k]; else next[k] = ids; onChange({ ...value, models: next }) }
  const drop = (k: 'scene_id' | 'detail_layout') => { const next = { ...value }; delete next[k]; onChange(next) }
  return <div className="stack">
    <div className="sub-title">模特</div>
    {(Object.keys(MODEL_SLOTS) as ModelSlotKey[]).map(k => {
      const own = k in ms
      return <div key={k} className="plan-ov">
        <span className="plan-ov-label">{MODEL_SLOTS[k]}</span>
        <label className="inline fs-12"><input type="checkbox" className="check" disabled={disabled} checked={own} onChange={e => setMs(k, e.target.checked ? [...(base.models[k] || [])] : null)} />单独指定</label>
        {own ? <ModelPicker models={options.models} value={ms[k] || []} disabled={disabled} onChange={ids => setMs(k, ids)} /> : <span className="t3 fs-12">沿用方案：{modelNames(base.models[k])}</span>}
      </div>
    })}
    <div className="form-row">
      <Field label="场景"><select className="input" disabled={disabled} value={'scene_id' in value ? String(value.scene_id || 0) : 'inherit'} onChange={e => e.target.value === 'inherit' ? drop('scene_id') : onChange({ ...value, scene_id: Number(e.target.value) || null })}>
        <option value="inherit">沿用方案（{sceneName(base.scene_id)}）</option>
        <option value="0">不指定（用店铺默认场景）</option>{options.scenes.map(s => <option key={s.id} value={s.id}>{s.name}</option>)}
      </select></Field>
      <Field label="详情页版式"><select className="input" disabled={disabled} value={'detail_layout' in value ? value.detail_layout || '' : 'inherit'} onChange={e => e.target.value === 'inherit' ? drop('detail_layout') : onChange({ ...value, detail_layout: e.target.value })}>
        <option value="inherit">沿用方案（{layoutName(base.detail_layout)}）</option>
        <option value="">店铺默认</option>{options.layouts.map(t => <option key={t.key} value={t.key}>{t.name}</option>)}
      </select></Field>
    </div>
    <div className="sub-title">模板</div>
    <div className="form-row plan-slots">
      {SLOTS.filter(s => tpls(s).length > 0).map(s => <Field key={s} label={slotLabel(s)}>
        <select className={'input' + (s in ts ? ' plan-own' : '')} disabled={disabled} value={s in ts ? ts[s] : 'inherit'} onChange={e => setTs(s, e.target.value === 'inherit' ? null : e.target.value)}>
          <option value="inherit">沿用方案（{tplName(base.template_set[s])}）</option>
          {tpls(s).map(t => <option key={t.key} value={t.key}>{t.name}</option>)}
        </select>
      </Field>)}
    </div>
  </div>
}
