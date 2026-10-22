import { useRef, useState, type PointerEvent as RPointerEvent } from 'react'
import { Package, Plus, Trash2 } from 'lucide-react'
import { PLATE_SIZES, PRODUCT_COLOR, PROP_COLORS, type Layout, type LayoutElement, type PlateSize } from '../lib/scenes'
import type { Prop } from '../lib/props'
import { Field } from './ui'

// 布局框：在画布上拖出商品框和道具框的位置和大小。定场图按这张色块示意图摆道具，商品框留空给之后放商品。
export function colorOf(layout: Layout, el: LayoutElement) {
  if (el.type === 'product') return { name: '洋红', hex: PRODUCT_COLOR }
  const i = layout.elements.filter(e => e.type === 'prop').indexOf(el)
  const [name, hex] = PROP_COLORS[i % PROP_COLORS.length]
  return { name, hex }
}

const MIN = 0.04
const clamp = (v: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, v))
const round = (v: number) => Math.round(v * 1000) / 1000

export function SceneLayout({ layout, onChange, props, disabled }: { layout: Layout; onChange: (l: Layout) => void; props: Prop[]; disabled?: boolean }) {
  const canvas = useRef<HTMLDivElement>(null)
  const [sel, setSel] = useState<string>('')
  const drag = useRef<{ id: string; mode: 'move' | 'size'; x: number; y: number; box: [number, number, number, number] } | null>(null)
  const [w, h] = layout.size.split('x').map(Number)
  const els = layout.elements
  const selected = els.find(e => e.id === sel)
  const setEl = (id: string, patch: Partial<LayoutElement>) => onChange({ ...layout, elements: els.map(e => e.id === id ? { ...e, ...patch } : e) })
  const nextId = (prefix: string) => { let n = 1; while (els.some(e => e.id === `${prefix}${n}`)) n++; return `${prefix}${n}` }
  const add = (el: Omit<LayoutElement, 'id' | 'box' | 'note'>) => {
    const id = el.type === 'product' ? 'product' : nextId('prop')
    const box: [number, number, number, number] = el.type === 'product' ? [0.35, 0.4, 0.3, 0.2] : [round(0.1 + (els.length % 4) * 0.05), round(0.55 + (els.length % 3) * 0.05), 0.3, 0.25]
    onChange({ ...layout, elements: [...els, { ...el, id, box, note: '' }] }); setSel(id)
  }
  const remove = (id: string) => { onChange({ ...layout, elements: els.filter(e => e.id !== id) }); setSel('') }

  const down = (e: RPointerEvent, el: LayoutElement, mode: 'move' | 'size') => {
    if (disabled) return
    e.stopPropagation(); e.preventDefault()
    setSel(el.id)
    drag.current = { id: el.id, mode, x: e.clientX, y: e.clientY, box: [...el.box] }
    ;(e.target as Element).setPointerCapture?.(e.pointerId)
  }
  const move = (e: RPointerEvent) => {
    const d = drag.current
    const rect = canvas.current?.getBoundingClientRect()
    if (!d || !rect) return
    const dx = (e.clientX - d.x) / rect.width, dy = (e.clientY - d.y) / rect.height
    const [x, y, bw, bh] = d.box
    const box: [number, number, number, number] = d.mode === 'move'
      ? [round(clamp(x + dx, 0, 1 - bw)), round(clamp(y + dy, 0, 1 - bh)), bw, bh]
      : [x, y, round(clamp(bw + dx, MIN, 1 - x)), round(clamp(bh + dy, MIN, 1 - y))]
    setEl(d.id, { box })
  }
  const up = () => { drag.current = null }

  const usable = props.filter(p => !p.archived)
  const hasProduct = els.some(e => e.type === 'product')
  return <div className="layout-edit">
    <div className="layout-tools">
      <select className="input input-sm" style={{ width: 110 }} disabled={disabled} value={layout.size} onChange={e => onChange({ ...layout, size: e.target.value as PlateSize })} aria-label="画布比例">
        {Object.entries(PLATE_SIZES).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
      </select>
      {!hasProduct && <button type="button" className="btn btn-secondary btn-sm" disabled={disabled} onClick={() => add({ type: 'product', label: '商品' })}><Package />加商品框</button>}
      <select className="input input-sm" style={{ width: 170 }} disabled={disabled || els.length >= 12} value="" onChange={e => { const p = usable.find(x => x.id === Number(e.target.value)); if (p) add({ type: 'prop', prop_id: p.id, label: p.name, prop_name: p.name, cover_url: p.cover_url }) }} aria-label="加道具">
        <option value="">{usable.length ? '＋ 加道具框…' : '道具库还没有道具'}</option>
        {usable.map(p => <option key={p.id} value={p.id}>{p.name}{p.cover_url ? '' : '（没锁定图）'}</option>)}
      </select>
      <span className="grow" /><span className="t4 fs-12 mono">{els.length}/12</span>
    </div>
    <div className="layout-body">
      <div ref={canvas} className={'layout-canvas' + (h > w ? ' portrait' : h === w ? ' square' : '')} style={{ aspectRatio: `${w} / ${h}` }} onPointerMove={move} onPointerUp={up} onPointerCancel={up} onPointerDown={() => setSel('')}>
        {!els.length && <div className="layout-hint">先加一个商品框，再把道具框拖到位置上</div>}
        {els.map(el => {
          const c = colorOf(layout, el)
          const [x, y, bw, bh] = el.box
          return <div key={el.id} className={'layout-box' + (el.type === 'product' ? ' product' : '') + (el.id === sel ? ' on' : '') + (disabled ? '' : ' movable')}
            style={{ left: `${x * 100}%`, top: `${y * 100}%`, width: `${bw * 100}%`, height: `${bh * 100}%`, borderColor: c.hex, background: c.hex + '33' }}
            onPointerDown={e => down(e, el, 'move')}>
            {el.cover_url && <img src={el.cover_url} alt="" draggable={false} />}
            <span className="layout-tag" style={{ background: c.hex }}>@{el.label || (el.type === 'product' ? '商品' : el.prop_name)}</span>
            {!disabled && <span className="layout-handle" style={{ background: c.hex }} onPointerDown={e => down(e, el, 'size')} />}
          </div>
        })}
      </div>
      <div className="layout-side">
        {selected ? <div className="stack" style={{ gap: 8 }}>
          <div className="row" style={{ gap: 6 }}><span className="layout-dot" style={{ background: colorOf(layout, selected).hex }} /><b className="fs-13">{selected.type === 'product' ? '商品框' : selected.prop_name || '道具'}</b><span className="t4 fs-12">{colorOf(layout, selected).name}</span></div>
          <Field label="叫什么（摆放说明里用 @ 引用）"><input className="input input-sm" maxLength={40} disabled={disabled} value={selected.label} onChange={e => setEl(selected.id, { label: e.target.value })} /></Field>
          <Field label="备注"><textarea className="textarea" rows={2} maxLength={200} disabled={disabled} value={selected.note} placeholder="如：斜放、只露一角" onChange={e => setEl(selected.id, { note: e.target.value })} /></Field>
          <div className="t4 fs-12 mono">x {selected.box[0].toFixed(2)} · y {selected.box[1].toFixed(2)} · {selected.box[2].toFixed(2)}×{selected.box[3].toFixed(2)}</div>
          {!disabled && <button type="button" className="btn btn-ghost btn-sm danger-text" style={{ alignSelf: 'flex-start' }} onClick={() => remove(selected.id)}><Trash2 />删掉这个框</button>}
        </div> : <div className="stack" style={{ gap: 6 }}>
          {els.map(el => <button key={el.id} type="button" className="layout-row" onClick={() => setSel(el.id)}>
            <span className="layout-dot" style={{ background: colorOf(layout, el).hex }} /><span className="ellipsis">@{el.label}</span>
          </button>)}
          <div className="help">{disabled ? '' : '拖框移动，拖右下角改大小；点框编辑名字和备注。'}商品框只能有一个，定场图里会留空。</div>
          {!disabled && !els.length && <button type="button" className="btn btn-secondary btn-sm" onClick={() => add({ type: 'product', label: '商品' })}><Plus />加商品框</button>}
        </div>}
      </div>
    </div>
  </div>
}

// 摆放说明：输入 @ 时弹出布局里的框名，点一下插进去；引用了布局里没有的名字会提示
export function BriefInput({ value, onChange, names, disabled }: { value: string; onChange: (v: string) => void; names: string[]; disabled?: boolean }) {
  const ref = useRef<HTMLTextAreaElement>(null)
  const [query, setQuery] = useState<string | null>(null)
  const sync = () => {
    const el = ref.current
    if (!el) return
    const m = /@([^\s@]*)$/.exec(el.value.slice(0, el.selectionStart))
    setQuery(m ? m[1] : null)
  }
  const insert = (name: string) => {
    const el = ref.current
    if (!el) return
    const pos = el.selectionStart
    const before = el.value.slice(0, pos).replace(/@([^\s@]*)$/, '')
    const at = query === null ? (before && !/\s$/.test(before) ? ' ' : '') : ''
    const next = before + at + '@' + name + ' ' + el.value.slice(pos)
    onChange(next); setQuery(null)
    requestAnimationFrame(() => { el.focus(); const c = (before + at + '@' + name + ' ').length; el.setSelectionRange(c, c) })
  }
  const hits = query === null ? [] : names.filter(n => n.includes(query))
  const unknown = [...new Set([...value.matchAll(/@([^\s@，。、,.;；]+)/g)].map(m => m[1]))].filter(n => !names.includes(n))
  return <div className="brief">
    <textarea ref={ref} className="textarea" rows={3} maxLength={1000} disabled={disabled} value={value} placeholder="如：@商品 放在 @胡桃木托盘 中央偏右，@亚麻布 从左下角垂进画面"
      onChange={e => { onChange(e.target.value); requestAnimationFrame(sync) }} onKeyUp={sync} onClick={sync} onBlur={() => setTimeout(() => setQuery(null), 150)}
      onKeyDown={e => { if (e.key === 'Escape') setQuery(null); if ((e.key === 'Enter' || e.key === 'Tab') && hits.length) { e.preventDefault(); insert(hits[0]) } }} />
    {hits.length > 0 && <div className="brief-pop">{hits.map(n => <button key={n} type="button" onMouseDown={e => { e.preventDefault(); insert(n) }}>@{n}</button>)}</div>}
    <div className="inline fs-12">
      {!disabled && names.map(n => <button key={n} type="button" className="btn btn-ghost btn-sm brief-chip" onClick={() => insert(n)}>@{n}</button>)}
      {unknown.length > 0 && <span className="views-issue">布局里没有：{unknown.map(n => '@' + n).join('、')}</span>}
    </div>
  </div>
}
