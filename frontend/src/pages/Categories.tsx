import { useEffect, useState } from 'react'
import { ArrowDown, ArrowUp, Gem, Plus, Trash2 } from 'lucide-react'
import { api } from '../lib/api'
import { useToast, errText } from '../lib/toast'
import { useCategories } from '../lib/categories'
import type { AttrField, AttrType, Category, DetailTemplate } from '../lib/types'
import { Pill, Loading, Drawer, Field, Tabs } from '../components/ui'

const TYPE_NAME: Record<AttrType, string> = { text: '单行文字', textarea: '多行文字', number: '数字', select: '单选', multi: '多选', bool: '是 / 否' }
const splitWords = (s: string) => s.split(/[、,，\n]+/).map(x => x.trim()).filter(Boolean)

export function CategoriesTab() {
  const { items, reload } = useCategories(true)
  const [edit, setEdit] = useState<Category | 'new' | null>(null)
  if (!items) return <Loading />
  return (
    <section className="panel">
      <div className="panel-head">
        <div className="titles"><div className="panel-title">品类</div><div className="panel-sub">每个品类有自己的属性、拍摄编号、出图槽位和文案规则。品类不能删，只能停用。</div></div>
        <span className="grow" />
        <button className="btn btn-primary btn-sm" onClick={() => setEdit('new')}><Plus />新建品类</button>
      </div>
      <div className="table-wrap"><table className="table">
        <thead><tr><th>品类</th><th className="num">属性</th><th className="num">拍摄编号</th><th>默认模板</th><th className="num">商品</th><th>状态</th></tr></thead>
        <tbody>{items.map(c => (
          <tr key={c.id} className="clickable" onClick={() => setEdit(c)}>
            <td><div className="cell-person"><span className="icon-tile sm"><Gem /></span><div><div className="cell-title">{c.name}</div><div className="cell-sub mono">{c.key}</div></div></div></td>
            <td className="num mono">{c.attrs.length}</td>
            <td className="num mono">{c.codes.length}</td>
            <td>{c.default_template || <span className="t4">跟店铺</span>}</td>
            <td className="num mono">{c.product_count ?? '—'}</td>
            <td>{c.disabled ? <Pill kind="pill-neutral">已停用</Pill> : <Pill kind="pill-success">使用中</Pill>}</td>
          </tr>
        ))}</tbody>
      </table></div>
      {edit && <CategoryDrawer cat={edit === 'new' ? null : edit} all={items} onClose={() => setEdit(null)} onDone={() => { setEdit(null); reload() }} />}
    </section>
  )
}

type Sec = 'basic' | 'attrs' | 'rules' | 'shots'

function CategoryDrawer({ cat, all, onClose, onDone }: { cat: Category | null; all: Category[]; onClose: () => void; onDone: () => void }) {
  const toast = useToast()
  const [sec, setSec] = useState<Sec>('basic')
  const [key, setKey] = useState('')
  const [copyFrom, setCopyFrom] = useState(all[0]?.key || '')
  const [name, setName] = useState(cat?.name || '')
  const [sort, setSort] = useState(String(cat?.sort ?? (Math.max(0, ...all.map(c => c.sort)) + 10)))
  const [disabled, setDisabled] = useState(!!cat?.disabled)
  const [tpl, setTpl] = useState(cat?.default_template || '')
  const [attrs, setAttrs] = useState<AttrField[]>(cat?.attrs || [])
  const [rules, setRules] = useState(cat?.rules || '')
  const [banned, setBanned] = useState((cat?.banned || []).join('、'))
  const [codes, setCodes] = useState(JSON.stringify(cat?.codes || [], null, 2))
  const [slots, setSlots] = useState(JSON.stringify(cat?.slots || {}, null, 2))
  const [templates, setTemplates] = useState<DetailTemplate[]>([])
  const [busy, setBusy] = useState(false)
  useEffect(() => { api<{ detail: DetailTemplate[] }>('/api/templates').then(r => setTemplates(r.detail || [])).catch(() => {}) }, [])

  const save = async () => {
    let codesV: unknown, slotsV: unknown
    try { codesV = JSON.parse(codes) } catch { setSec('shots'); toast('拍摄编号不是合法的 JSON', 'error'); return }
    try { slotsV = JSON.parse(slots) } catch { setSec('shots'); toast('槽位不是合法的 JSON', 'error'); return }
    const cleanAttrs = attrs.map(a => ({ ...a, key: a.key.trim(), label: a.label.trim(), options: a.type === 'select' || a.type === 'multi' ? (a.options || []).filter(Boolean) : undefined }))
    setBusy(true)
    try {
      if (cat) {
        await api(`/api/categories/${cat.id}`, { method: 'PATCH', body: {
          name: name.trim(), sort: Number(sort) || 0, disabled, default_template: tpl, attrs: cleanAttrs, rules, banned: splitWords(banned), codes: codesV, slots: slotsV
        } })
      } else {
        await api('/api/categories', { body: { key: key.trim(), name: name.trim(), sort: Number(sort) || 0, copy_from: copyFrom || undefined } })
      }
      toast('已保存', 'ok'); onDone()
    } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }

  const foot = <>
    <button className="btn btn-secondary" onClick={onClose}>取消</button>
    <button className="btn btn-primary" disabled={busy || !name.trim() || (!cat && !key.trim())} onClick={save}>保存</button>
  </>

  if (!cat) return (
    <Drawer title="新建品类" width={420} onClose={onClose} foot={foot}>
      <div className="form-row">
        <Field label="名称" req><input className="input" autoFocus value={name} onChange={e => setName(e.target.value)} placeholder="如：玉石" /></Field>
        <Field label="英文标识" req hint="小写英文、数字、下划线，建好不能改"><input className="input mono" value={key} onChange={e => setKey(e.target.value)} placeholder="jade" /></Field>
      </div>
      <Field label="从哪个品类复制" hint="复制它的属性、拍摄编号、槽位、规则和默认模板，建好后再改。">
        <select className="input" value={copyFrom} onChange={e => setCopyFrom(e.target.value)}>
          <option value="">不复制，从空白开始</option>
          {all.map(c => <option key={c.id} value={c.key}>{c.name}</option>)}
        </select>
      </Field>
      <Field label="排序" hint="小的排前面"><input className="input mono" inputMode="numeric" value={sort} onChange={e => setSort(e.target.value)} /></Field>
    </Drawer>
  )

  return (
    <Drawer title={`编辑品类 · ${cat.name}`} width={880} onClose={onClose} foot={foot}>
      <Tabs value={sec} onChange={setSec} items={[
        { key: 'basic', label: '基本' },
        { key: 'attrs', label: '属性', count: attrs.length },
        { key: 'rules', label: '文案规则' },
        { key: 'shots', label: '拍摄和槽位' }
      ]} />
      {sec === 'basic' && <>
        <div className="form-row">
          <Field label="名称" req><input className="input" value={name} onChange={e => setName(e.target.value)} /></Field>
          <Field label="英文标识"><input className="input mono" value={cat.key} disabled /></Field>
        </div>
        <div className="form-row">
          <Field label="默认详情页模板" hint="店铺没设默认、或店铺默认不适用于这个品类时用它">
            <select className="input" value={tpl} onChange={e => setTpl(e.target.value)}>
              <option value="">不指定</option>
              {templates.map(t => <option key={t.key} value={t.key}>{t.name}</option>)}
            </select>
          </Field>
          <Field label="排序" hint="小的排前面"><input className="input mono" inputMode="numeric" value={sort} onChange={e => setSort(e.target.value)} /></Field>
        </div>
        <label className="setting" style={{ cursor: 'pointer' }}>
          <div><div className="cell-title">停用这个品类</div><div className="fs-12 t3">停用后不能新建这个品类的商品，已有的 {cat.product_count ?? 0} 款照常用。</div></div>
          <input type="checkbox" className="switch" checked={disabled} onChange={e => setDisabled(e.target.checked)} />
        </label>
      </>}
      {sec === 'attrs' && <AttrsEditor attrs={attrs} onChange={setAttrs} />}
      {sec === 'rules' && <>
        <Field label="品类规则" hint="Markdown。插进分拣、文案、改写、合规审查的提示词里：叫法和材质、详情页第 2 屏怎么写、分拣要点、合规重点。">
          <textarea className="textarea code-area" rows={18} value={rules} onChange={e => setRules(e.target.value)} />
        </Field>
        <Field label="品类违禁词" hint="用顿号或换行分开。和全店违禁词合并检查，命中算必须改。">
          <textarea className="textarea" rows={3} value={banned} onChange={e => setBanned(e.target.value)} />
        </Field>
      </>}
      {sec === 'shots' && <>
        <div className="fs-12 t3">一般不用改。改之前先确认：槽位的「候选」只能写拍摄编号里有的编号；已分拣的素材编号不会跟着变。</div>
        <Field label="拍摄编号（JSON）" hint='每项 {"code","group","name","use","desc","suffix"}，code 如 A1；suffix 有值表示可以写成 A3_白 这样'>
          <textarea className="textarea code-area" rows={12} value={codes} onChange={e => setCodes(e.target.value)} spellCheck={false} />
        </Field>
        <Field label="出图槽位（JSON）" hint="格式同 槽位.json：主图、详情页两组，每个位置按优先级列候选编号">
          <textarea className="textarea code-area" rows={12} value={slots} onChange={e => setSlots(e.target.value)} spellCheck={false} />
        </Field>
      </>}
    </Drawer>
  )
}

function AttrsEditor({ attrs, onChange }: { attrs: AttrField[]; onChange: (a: AttrField[]) => void }) {
  const up = (i: number, patch: Partial<AttrField>) => onChange(attrs.map((a, j) => (j === i ? { ...a, ...patch } : a)))
  const move = (i: number, d: number) => {
    const j = i + d
    if (j < 0 || j >= attrs.length) return
    const next = [...attrs];[next[i], next[j]] = [next[j], next[i]]; onChange(next)
  }
  return (
    <div className="stack">
      <div className="fs-12 t3">商品的基础信息按这里的字段填。标识用小写英文，改了标识老商品里的值就对不上，已在用的字段别改标识。必填只是提醒，不填也能保存。</div>
      <div className="table-wrap"><table className="table attr-table">
        <thead><tr><th style={{ width: 110 }}>名称</th><th style={{ width: 110 }}>标识</th><th style={{ width: 112 }}>类型</th><th>选项 / 单位 / 提示</th><th style={{ width: 52 }}>必填</th><th style={{ width: 84 }} /></tr></thead>
        <tbody>{attrs.map((a, i) => {
          const hasOpts = a.type === 'select' || a.type === 'multi'
          return (
            <tr key={i}>
              <td><input className="input" value={a.label} onChange={e => up(i, { label: e.target.value })} /></td>
              <td><input className="input mono" value={a.key} onChange={e => up(i, { key: e.target.value })} /></td>
              <td><select className="input" value={a.type} onChange={e => up(i, { type: e.target.value as AttrType })}>
                {Object.entries(TYPE_NAME).map(([k, v]) => <option key={k} value={k}>{v}</option>)}</select></td>
              <td><div className="stack" style={{ gap: 4 }}>
                {hasOpts && <input className="input" placeholder="选项，用顿号分开" value={(a.options || []).join('、')} onChange={e => up(i, { options: splitWords(e.target.value) })} />}
                <div className="inline" style={{ gap: 4 }}>
                  <input className="input" style={{ width: 72 }} placeholder="单位" value={a.unit || ''} onChange={e => up(i, { unit: e.target.value || undefined })} />
                  <input className="input" placeholder="填写提示" value={a.hint || ''} onChange={e => up(i, { hint: e.target.value || undefined })} />
                </div>
                {hasOpts && <label className="fs-12 t3 inline" style={{ gap: 6 }}><input type="checkbox" className="check" checked={!!a.free} onChange={e => up(i, { free: e.target.checked || undefined })} />允许填选项以外的值</label>}
              </div></td>
              <td><input type="checkbox" className="check" checked={!!a.required} onChange={e => up(i, { required: e.target.checked || undefined })} /></td>
              <td><div className="inline" style={{ gap: 2 }}>
                <button className="btn btn-ghost btn-icon btn-sm" title="上移" onClick={() => move(i, -1)}><ArrowUp /></button>
                <button className="btn btn-ghost btn-icon btn-sm" title="下移" onClick={() => move(i, 1)}><ArrowDown /></button>
                <button className="btn btn-ghost btn-icon btn-sm" title="删除" onClick={() => onChange(attrs.filter((_, j) => j !== i))}><Trash2 /></button>
              </div></td>
            </tr>
          )
        })}</tbody>
      </table></div>
      <div><button className="btn btn-secondary btn-sm" onClick={() => onChange([...attrs, { key: '', label: '', type: 'text' }])}><Plus />加一个字段</button></div>
    </div>
  )
}
