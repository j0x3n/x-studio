import { useState } from 'react'
import type { AttrField } from '../lib/types'

type Info = Record<string, unknown>

// 按品类的属性模板生成表单。数值存 number，多选存数组，空值删掉这个 key
export function AttrFields({ attrs, value, onChange }: { attrs: AttrField[]; value: Info; onChange: (v: Info) => void }) {
  const set = (k: string, v: unknown) => {
    const next = { ...value }
    if (v === '' || v === undefined || (Array.isArray(v) && v.length === 0)) delete next[k]
    else next[k] = v
    onChange(next)
  }
  return (
    <div className="attr-grid">
      {attrs.map(a => {
        // 多选里是一排按钮，用 label 包会让点标题时误点第一个选项
        const Wrap = a.type === 'multi' ? 'div' : 'label'
        return (
          <Wrap key={a.key} className={'form-field' + (a.type === 'textarea' || a.type === 'multi' ? ' wide' : '')}>
            <span className={'label' + (a.required ? ' req' : '')}>{a.label}{a.unit ? <span className="t4">（{a.unit}）</span> : null}</span>
            <AttrInput a={a} v={value[a.key]} set={v => set(a.key, v)} />
            {a.hint && <span className="help">{a.hint}</span>}
          </Wrap>
        )
      })}
    </div>
  )
}

function AttrInput({ a, v, set }: { a: AttrField; v: unknown; set: (v: unknown) => void }) {
  const str = v === undefined || v === null ? '' : String(v)
  switch (a.type) {
    case 'textarea':
      return <textarea className="textarea" rows={2} value={str} onChange={e => set(e.target.value)} />
    case 'number':
      return <input className="input mono" inputMode="decimal" value={str} onChange={e => {
        const t = e.target.value.trim()
        set(t === '' ? '' : Number.isFinite(Number(t)) ? Number(t) : t)
      }} />
    case 'bool':
      return <select className="input" value={v === true ? '1' : v === false ? '0' : ''} onChange={e => set(e.target.value === '' ? '' : e.target.value === '1')}>
        <option value="">不填</option><option value="1">是</option><option value="0">否</option>
      </select>
    case 'select':
      if (a.free) {
        const id = `attr-${a.key}`
        return <><input className="input" list={id} value={str} onChange={e => set(e.target.value)} placeholder="选或直接填" />
          <datalist id={id}>{(a.options || []).map(o => <option key={o} value={o} />)}</datalist></>
      }
      return <select className="input" value={str} onChange={e => set(e.target.value)}>
        <option value="">不填</option>
        {(a.options || []).map(o => <option key={o} value={o}>{o}</option>)}
        {str && !(a.options || []).includes(str) && <option value={str}>{str}</option>}
      </select>
    case 'multi':
      return <MultiInput a={a} v={Array.isArray(v) ? v.map(String) : typeof v === 'string' && v ? v.split(/[、,，\s]+/).filter(Boolean) : []} set={set} />
    default:
      return <input className="input" value={str} onChange={e => set(e.target.value)} />
  }
}

function MultiInput({ a, v, set }: { a: AttrField; v: string[]; set: (v: string[]) => void }) {
  const [extra, setExtra] = useState('')
  const opts = [...(a.options || []), ...v.filter(x => !(a.options || []).includes(x))]
  const toggle = (o: string) => set(v.includes(o) ? v.filter(x => x !== o) : [...v, o])
  const add = () => { const t = extra.trim(); if (t && !v.includes(t)) set([...v, t]); setExtra('') }
  return (
    <div className="chip-pick">
      {opts.map(o => <button type="button" key={o} className={'chip' + (v.includes(o) ? ' on' : '')} onClick={() => toggle(o)}>{o}</button>)}
      {a.free && <input className="input chip-add" value={extra} placeholder="其他，回车添加" onChange={e => setExtra(e.target.value)}
        onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); add() } }} onBlur={add} />}
    </div>
  )
}
