import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { Pencil, ArrowDownToLine, ArrowUpFromLine, ArrowRightLeft, Truck, Plus, AlertTriangle } from 'lucide-react'
import { api, qs, type Page } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { useCategories } from '../lib/categories'
import { fmtDate } from '../lib/format'
import { kindName, whTypeName, refName, yuan, qtyText, specText, availText, type ItemDetail, type Dropship, type InvItem } from '../lib/inventory'
import type { KbArticle } from '../lib/kb'
import { Topbar, Loading, Empty } from '../components/ui'
import { MoveDrawer, TransferDrawer, ItemDrawer, DropshipDrawer, useCostPerm, useWarehouses } from '../components/InventoryForms'
import { LedgerTable } from './Inventory'

// 后端详情里代发行可能叫 dropship（数组），列表里 dropship 是数字；统一成 dropship_rows
function norm(r: unknown): ItemDetail {
  const o = (r && typeof r === 'object' ? ((r as { item?: object }).item ? { ...(r as { item: object }).item, ...(r as object) } : r) : {}) as Record<string, unknown>
  const rows = Array.isArray(o.dropship) ? (o.dropship as Dropship[]) : (o.dropship_rows as Dropship[]) || []
  return {
    ...(o as unknown as ItemDetail), dropship: Array.isArray(o.dropship) ? rows.reduce((s, d) => s + (d.qty || 0), 0) : Number(o.dropship) || 0,
    dropship_rows: rows, levels: (o.levels as ItemDetail['levels']) || [], batches: (o.batches as ItemDetail['batches']) || [],
    reservations: (o.reservations as ItemDetail['reservations']) || [], ledger: (o.ledger as ItemDetail['ledger']) || [], spec: (o.spec as Record<string, string>) || {}, units: (o.units as InvItem['units']) || []
  }
}

export function InventoryItem() {
  const { id } = useParams()
  const nav = useNavigate()
  const { can } = useAuth()
  const toast = useToast()
  const costPerm = useCostPerm()
  const edit = can('inventory.edit')
  const { items: cats } = useCategories(true)
  const { items: whs } = useWarehouses(true)
  const [d, setD] = useState<ItemDetail | null>(null)
  const [drawer, setDrawer] = useState<'' | 'edit' | 'in' | 'out' | 'transfer' | 'drop'>('')
  const [drop, setDrop] = useState<Dropship | null>(null)
  const [kb, setKb] = useState<KbArticle[]>([])
  const load = useCallback(() => {
    api(`/api/inventory/items/${id}`).then(r => setD(norm(r))).catch(e => { toast(errText(e), 'error'); nav('/inventory') })
  }, [id, nav, toast])
  useEffect(() => { load() }, [load])
  useEffect(() => {
    if (!can('kb.view')) return
    api<Page<KbArticle>>('/api/kb/articles' + qs({ target_type: 'item', target_id: id, page_size: 20 })).then(r => setKb(r.items || [])).catch(() => setKb([]))
  }, [id]) // eslint-disable-line react-hooks/exhaustive-deps
  if (!d) return <><Topbar crumbs={['库存', '…']} /><Loading /></>
  const done = () => { setDrawer(''); setDrop(null); load() }
  const whName = (wid: number) => whs?.find(w => w.id === wid)?.name || `#${wid}`
  const cat = cats?.find(c => c.id === d.category_id)
  const levels = d.levels.filter(l => (l.on_hand ?? l.qty ?? 0) !== 0)

  return (
    <>
      <Topbar crumbs={[<Link key="i" to="/inventory">库存</Link>]} title={<><span className="mono">{d.code}</span> {d.name}</>} sub={<>{kindName[d.kind]}{cat && ` · ${cat.name}`}{d.archived && ' · 已归档'}</>}>
        {edit && !d.archived && <>
          <button className="btn btn-secondary" onClick={() => setDrawer('in')}><ArrowDownToLine />入库</button>
          <button className="btn btn-secondary" onClick={() => setDrawer('out')}><ArrowUpFromLine />出库</button>
          <button className="btn btn-secondary" onClick={() => setDrawer('transfer')}><ArrowRightLeft />调拨</button>
        </>}
        {edit && <button className="btn btn-primary" onClick={() => setDrawer('edit')}><Pencil />修改</button>}
      </Topbar>
      <div className="scroll"><main className="content stack">
        <div className="stat-strip panel">
          <div><div className="kpi-label">现有（自有仓 + 档口）</div><div className="kpi-value mono">{d.on_hand}</div>{d.units.length > 0 && <div className="t3 fs-12">{qtyText(d.on_hand, d)}</div>}</div>
          <div><div className="kpi-label">代发可售</div><div className="kpi-value mono">{d.unlimited ? '不限' : d.dropship}</div></div>
          <div><div className="kpi-label">订单占用</div><div className="kpi-value mono">{d.reserved}</div></div>
          <div><div className="kpi-label">可售</div><div className={'kpi-value mono' + (d.low ? ' danger-text' : '')}>{availText(d)}{d.low && <AlertTriangle className="inv-low-ico" />}</div><div className="t3 fs-12">安全库存 {d.safety_stock || '不提醒'}</div></div>
          {costPerm && <div><div className="kpi-label">库存成本</div><div className="kpi-value mono">{yuan(d.cost_value)}</div></div>}
        </div>
        <div className="inv-detail">
          <div className="stack">
            <section className="panel">
              <div className="panel-head"><div className="panel-title">各仓库存</div></div>
              {levels.length === 0 ? <Empty title="没有库存" /> : (
                <div className="table-wrap"><table className="table">
                  <thead><tr><th>仓库</th><th>类型</th><th className="num">现有</th></tr></thead>
                  <tbody>{levels.map(l => <tr key={l.warehouse_id}><td>{l.warehouse_name || l.name || whName(l.warehouse_id)}</td><td className="t2">{l.type ? whTypeName[l.type] : ''}</td><td className="num mono">{qtyText(l.on_hand ?? l.qty ?? 0, d)}</td></tr>)}</tbody>
                </table></div>
              )}
            </section>
            <section className="panel">
              <div className="panel-head"><div className="titles"><div className="panel-title">批次</div><div className="panel-sub">出库默认先进先出（按到货日期从早到晚）</div></div></div>
              {d.batches.length === 0 ? <Empty title="没有剩余的批次" /> : (
                <div className="table-wrap"><table className="table">
                  <thead><tr><th>批次</th><th>仓库</th><th>等级</th><th>到货</th><th className="num">入库</th><th className="num">剩余</th>{costPerm && <><th className="num">单位成本</th><th className="num">剩余成本</th></>}<th>来源</th></tr></thead>
                  <tbody>{d.batches.map(b => (
                    <tr key={b.id}>
                      <td className="mono">#{b.id}</td><td>{b.warehouse_name || whName(b.warehouse_id)}</td><td>{b.grade || <span className="t4">—</span>}</td><td className="t2">{fmtDate(b.received_at)}</td>
                      <td className="num mono t3">{b.qty_in}</td><td className="num mono">{b.qty_left}</td>
                      {costPerm && <><td className="num mono t2">{b.qty_in ? yuan(Math.round((b.cost_in || 0) / b.qty_in)) : '—'}</td><td className="num mono">{yuan(b.cost_left)}</td></>}
                      <td className="t3">{refName[b.ref_type] || b.ref_type}</td>
                    </tr>
                  ))}</tbody>
                </table></div>
              )}
            </section>
            <section className="panel">
              <div className="panel-head"><div className="titles"><div className="panel-title">最近流水</div></div><span className="grow" /><Link className="link fs-12" to={`/inventory?tab=ledger`}>全部流水</Link></div>
              {d.ledger.length === 0 ? <Empty title="还没有出入库记录" /> : <LedgerTable rows={d.ledger} showItem={false} />}
            </section>
          </div>
          <aside className="stack">
            <section className="panel panel-section inv-props">
              <div className="section-title">资料</div>
              <dl>
                <dt>挂在商品</dt><dd>{d.product ? (d.product.name ? <Link className="link" to={`/products/${d.product.id}`}>{d.product.name}</Link> : <span className="t4">无权查看</span>) : '—'}</dd>
                <dt>规格</dt><dd>{specText(d.spec) || '—'}</dd>
                <dt>单位</dt><dd>{d.unit}{d.units.length > 0 && <span className="t3">（{d.units.map(u => `1 ${u.name} = ${u.factor} ${d.unit}`).join('，')}）</span>}</dd>
                {d.note && <><dt>备注</dt><dd className="pre">{d.note}</dd></>}
              </dl>
            </section>
            <section className="panel panel-section">
              <div className="between"><div className="section-title">代发</div>{edit && <button className="btn btn-ghost btn-sm" onClick={() => { setDrop(null); setDrawer('drop') }}><Plus />添加</button>}</div>
              {(d.dropship_rows || []).length === 0 ? <div className="t4 fs-12" style={{ marginTop: 6 }}>没有代发货源</div> : (
                <div className="list">{(d.dropship_rows || []).map(r => (
                  <div key={r.warehouse_id} className={'list-item' + (edit ? ' clickable' : '')} onClick={() => { if (edit) { setDrop(r); setDrawer('drop') } }}>
                    <Truck className="t3" style={{ width: 14 }} /><span className="title grow">{r.warehouse_name || whName(r.warehouse_id)}</span>
                    <span className="mono">{r.qty === null ? '不限' : r.qty}</span>{costPerm && <span className="t3 mono">{yuan(r.unit_cost)}</span>}
                  </div>
                ))}</div>
              )}
            </section>
            {d.reservations.length > 0 && (
              <section className="panel panel-section">
                <div className="section-title">占用</div>
                <div className="list">{d.reservations.map(r => (
                  <div key={r.ref_type + r.ref_id} className="list-item">
                    {r.ref_path ? <Link className="link title grow" to={r.ref_path}>{r.ref_label || `${r.ref_type} #${r.ref_id}`}</Link> : <span className="title grow">{r.ref_label || `${r.ref_type} #${r.ref_id}`}</span>}
                    <span className="mono">{r.qty}</span>
                  </div>
                ))}</div>
              </section>
            )}
            {can('kb.view') && (
              <section className="panel panel-section">
                <div className="section-title">相关文章</div>
                {kb.length === 0 ? <div className="t4 fs-12" style={{ marginTop: 6 }}>知识库里还没有关联这个物料的文章</div> : (
                  <div className="list">{kb.map(a => <Link key={a.id} className="list-item link" to={`/kb/${a.id}`}><span className="ellipsis">{a.title}</span></Link>)}</div>
                )}
              </section>
            )}
          </aside>
        </div>
      </main></div>
      {drawer === 'edit' && <ItemDrawer item={d} onClose={() => setDrawer('')} onDone={done} />}
      {drawer === 'in' && <MoveDrawer kind="other_in" item={d} onClose={() => setDrawer('')} onDone={done} />}
      {drawer === 'out' && <MoveDrawer kind="other_out" item={d} onClose={() => setDrawer('')} onDone={done} />}
      {drawer === 'transfer' && <TransferDrawer item={d} onClose={() => setDrawer('')} onDone={done} />}
      {drawer === 'drop' && <DropshipDrawer item={d} row={drop} onClose={() => setDrawer('')} onDone={done} />}
    </>
  )
}
