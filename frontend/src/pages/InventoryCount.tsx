import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { Check, X, Plus } from 'lucide-react'
import { api } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { fmtDate } from '../lib/format'
import { countStatus, toFen, fenToInput, type Count, type CountLine, type InvItem } from '../lib/inventory'
import { Topbar, Loading, Empty, Pill } from '../components/ui'
import { ItemPicker, useCostPerm, useWarehouses } from '../components/InventoryForms'

type Detail = Count & { lines: CountLine[] }
const norm = (r: unknown): Detail => {
  const o = (r && typeof r === 'object' ? r : {}) as Record<string, unknown>
  const c = (o.count && typeof o.count === 'object' ? { ...(o.count as object), ...o } : o) as unknown as Detail
  return { ...c, lines: c.lines || [] }
}

// 盘点单：逐行填实盘数，随时保存，完成时生成盘盈盘亏
export function InventoryCount() {
  const { id } = useParams()
  const { can } = useAuth()
  const toast = useToast()
  const costPerm = useCostPerm()
  const { items: whs } = useWarehouses(true)
  const [d, setD] = useState<Detail | null>(null)
  const [edits, setEdits] = useState<Record<number, { counted: string; cost: string }>>({})
  const [adding, setAdding] = useState<InvItem | null>(null)
  const [busy, setBusy] = useState(false)
  const [q, setQ] = useState('')
  const load = useCallback(() => { api(`/api/inventory/counts/${id}`).then(r => { setD(norm(r)); setEdits({}) }).catch(e => toast(errText(e), 'error')) }, [id, toast])
  useEffect(() => { load() }, [load])
  const draft = d?.status === 'draft' && can('inventory.edit')
  const val = (l: CountLine) => edits[l.item_id]?.counted ?? (l.counted_qty == null ? '' : String(l.counted_qty))
  const costVal = (l: CountLine) => edits[l.item_id]?.cost ?? fenToInput(l.unit_cost)
  const lines = useMemo(() => (d?.lines || []).filter(l => !q || l.code.toLowerCase().includes(q.toLowerCase()) || l.name.includes(q)), [d, q])
  if (!d) return <><Topbar crumbs={['库存', '盘点', '…']} /><Loading /></>
  const dirty = Object.keys(edits).length > 0
  const counted = d.lines.filter(l => val(l) !== '').length
  // 盘点中按「实盘 − 当前现有」算（完成时就按这个生成流水）；完成后当前现有已经变了，只能对开盘时的快照
  const diffOf = (l: CountLine) => {
    if (d.status !== 'draft') return l.counted_qty == null ? null : l.counted_qty - l.system_qty
    const v = val(l); return v === '' ? null : Number(v) - l.current_qty
  }
  const gain = d.lines.reduce((s, l) => s + Math.max(0, diffOf(l) ?? 0), 0)
  const loss = d.lines.reduce((s, l) => s + Math.min(0, diffOf(l) ?? 0), 0)

  const set = (l: CountLine, p: Partial<{ counted: string; cost: string }>) => setEdits(e => ({ ...e, [l.item_id]: { ...(e[l.item_id] || { counted: val(l), cost: costVal(l) }), ...p } }))
  const save = async (extra?: number[]) => {
    const body = {
      lines: Object.entries(edits).map(([k, v]) => ({ item_id: Number(k), counted_qty: v.counted === '' ? null : Number(v.counted), ...(costPerm ? { unit_cost: v.cost ? toFen(v.cost) : null } : {}) })),
      ...(extra ? { add_item_ids: extra } : {})
    }
    await api(`/api/inventory/counts/${id}`, { method: 'PATCH', body })
  }
  const run = async (f: () => Promise<unknown>, msg?: string) => {
    setBusy(true)
    try { await f(); if (msg) toast(msg, 'ok'); load() } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const finish = () => {
    const skip = d.lines.length - counted
    if (!confirm(`完成盘点？${skip ? `还有 ${skip} 行没填，会跳过。` : ''}会按实盘数生成盘盈 ${gain}、盘亏 ${-loss} 的流水，完成后不能再改。`)) return
    run(async () => { if (dirty) await save(); await api(`/api/inventory/counts/${id}/finish`, { method: 'POST' }) }, '盘点完成')
  }
  const cancel = () => { if (confirm('取消这张盘点单？不会生成任何流水。')) run(() => api(`/api/inventory/counts/${id}/cancel`, { method: 'POST' })) }
  const add = () => { if (adding) run(async () => { await save([adding.id]); setAdding(null) }) }

  return (
    <>
      <Topbar crumbs={[<Link key="i" to="/inventory">库存</Link>, <Link key="c" to="/inventory?tab=counts">盘点</Link>]} title={`盘点 #${d.id} · ${d.warehouse_name || whs?.find(w => w.id === d.warehouse_id)?.name || ''}`}
        sub={<><Pill kind={countStatus[d.status].pill}>{countStatus[d.status].label}</Pill><span className="sep" />{fmtDate(d.created_at, true)} 开始{d.note && ` · ${d.note}`}</>}>
        {draft && <>
          <button className="btn btn-ghost" disabled={busy} onClick={cancel}><X />取消盘点</button>
          <button className="btn btn-secondary" disabled={busy || !dirty} onClick={() => run(() => save(), '已保存')}>保存</button>
          <button className="btn btn-primary" disabled={busy} onClick={finish}><Check />完成盘点</button>
        </>}
      </Topbar>
      <div className="scroll"><main className="content">
        <div className="toolbar">
          <span className="t2">已盘 <b className="mono">{counted}</b> / {d.lines.length}</span>
          <span className="sep" /><span className="inv-in mono">盘盈 +{gain}</span><span className="inv-out mono">盘亏 {loss}</span>
          {dirty && <span className="tag tag-orange">有改动没保存</span>}
          <span className="spacer" />
          {draft && <div className="inv-count-add"><ItemPicker value={adding} onChange={setAdding} /><button className="btn btn-secondary btn-sm" disabled={!adding || busy} onClick={add}><Plus />加进盘点</button></div>}
          <input className="input" style={{ width: 180 }} placeholder="筛选编码或名称" value={q} onChange={e => setQ(e.target.value)} />
        </div>
        <section className="panel">
          {lines.length === 0 ? <Empty title="没有物料" /> : (
            <div className="table-wrap"><table className="table">
              <thead><tr><th>物料</th><th className="num">开盘时</th><th className="num">当前现有</th><th className="num" style={{ width: 130 }}>实盘数</th><th className="num">{d.status === 'draft' ? '差异' : '差异（对开盘时）'}</th>{costPerm && <th className="num" style={{ width: 140 }}>盘盈单位成本（元）</th>}</tr></thead>
              <tbody>{lines.map(l => {
                const diff = diffOf(l)
                return (
                  <tr key={l.item_id}>
                    <td className="wrap"><Link className="link" to={`/inventory/items/${l.item_id}`}><span className="mono">{l.code}</span> {l.name}</Link></td>
                    <td className="num mono t3">{l.system_qty}</td>
                    <td className="num mono">{l.current_qty}{d.status === 'draft' && l.current_qty !== l.system_qty && <span className="t4" title="盘点期间有出入库"> *</span>}</td>
                    <td className="num">{draft ? <input className="input mono inv-count-input" inputMode="numeric" value={val(l)} placeholder="未盘" onChange={e => set(l, { counted: e.target.value.replace(/\D/g, '') })} /> : <span className="mono">{l.counted_qty ?? <span className="t4">未盘</span>}</span>}</td>
                    <td className={'num mono ' + (diff == null || diff === 0 ? 't4' : diff > 0 ? 'inv-in' : 'inv-out')}>{diff == null ? '—' : diff > 0 ? `+${diff}` : diff}</td>
                    {costPerm && <td className="num">{draft && (diff ?? 0) > 0 ? <input className="input mono inv-count-input" inputMode="decimal" placeholder="按最近批次" value={costVal(l)} onChange={e => set(l, { cost: e.target.value })} /> : <span className="t4">—</span>}</td>}
                  </tr>
                )
              })}</tbody>
            </table></div>
          )}
        </section>
      </main></div>
    </>
  )
}
