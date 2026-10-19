import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { Pencil, PackageCheck } from 'lucide-react'
import { api } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { fmtDate } from '../lib/format'
import { money, purchaseStatus, type Purchase } from '../lib/commerce'
import { Topbar, Empty, Pill, Drawer, Field } from '../components/ui'
import { DocumentPanel, RecordState, useRecord } from '../components/CommerceShared'
import { PurchaseForm, ReceiptForm } from '../components/PurchaseForms'

export function PurchaseDetail() {
  const { id } = useParams()
  const { can } = useAuth()
  const toast = useToast()
  const { data: p, error, reload } = useRecord<Purchase>(`/api/purchase-orders/${id}`)
  const [drawer, setDrawer] = useState('')
  const [busy, setBusy] = useState(false)
  if (!can('purchase.manage')) return <><Topbar title="采购单" /><Empty title="没有管理采购的权限" /></>
  if (!p) return <><Topbar crumbs={[<Link to="/purchase-orders">采购单</Link>, '详情']} /><RecordState error={error} reload={reload} /></>
  const action = async (path: string, message: string, body?: unknown) => {
    setBusy(true)
    try { await api(path, { method: body ? 'PATCH' : 'POST', body }); toast(message, 'ok'); setDrawer(''); reload() }
    catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const active = !['done', 'canceled'].includes(p.status)
  const sum = p.lines.reduce((s, l) => s + l.amount, 0)
  const lineName = (lineID: number) => { const l = p.lines.find(l => l.id === lineID); return l ? `${l.code} · ${l.name}` : `明细 #${lineID}` }
  return <>
    <Topbar crumbs={[<Link to="/purchase-orders">采购单</Link>]} title={<span className="mono">{p.no}</span>} sub={<Pill kind={purchaseStatus[p.status].pill}>{purchaseStatus[p.status].label}</Pill>}>
      {active && <button className="btn btn-secondary" onClick={() => setDrawer(p.status === 'draft' ? 'edit' : 'note')}><Pencil />{p.status === 'draft' ? '编辑草稿' : '改备注 / 日期'}</button>}
      {['draft', 'ordered'].includes(p.status) && <button className="btn btn-danger" disabled={busy} onClick={() => setDrawer('cancel')}>取消采购</button>}
      {['partial', 'received'].includes(p.status) && <button className={'btn ' + (p.status === 'received' ? 'btn-primary' : 'btn-secondary')} disabled={busy} onClick={() => setDrawer('finish')}>完成采购</button>}
      {p.status === 'draft' && <button className="btn btn-primary" disabled={busy} onClick={() => setDrawer('order')}>确认下单</button>}
      {['ordered', 'partial'].includes(p.status) && <button className="btn btn-primary" onClick={() => setDrawer('receipt')}><PackageCheck />登记到货</button>}
    </Topbar>
    <div className="scroll"><main className="content commerce-detail">
      <div className="stack">
        <DocumentPanel title="采购明细"><div className="table-wrap"><table className="table"><thead><tr><th>物料</th><th className="num">订购</th><th className="num">累计实收</th><th className="num">不合格</th><th className="num">货款</th><th className="num">分摊运费</th></tr></thead><tbody>{p.lines.map(l => <tr key={l.id}><td className="wrap"><Link className="link" to={`/inventory/items/${l.item_id}`}><span className="mono">{l.code}</span> {l.name}</Link></td><td className="num mono">{l.qty} {l.unit}</td><td className="num mono">{l.received_qty}</td><td className={'num mono' + (l.rejected_qty ? ' danger-text' : ' t3')}>{l.rejected_qty || '—'}</td><td className="num mono">{money(l.amount, p.currency)}</td><td className="num mono t2">{money(l.shipping_share, p.currency)}</td></tr>)}</tbody></table></div><div className="pager">货款 <span className="mono">{money(sum, p.currency)}</span><span className="spacer" />含运费合计 <strong className="mono">{money(sum + p.shipping_fee, p.currency)}</strong></div></DocumentPanel>
        <DocumentPanel title={`到货记录 · ${p.receipts.length} 次`}>{!p.receipts.length ? <Empty title="尚未登记到货" desc="下单后可分批登记实收、质检等级和不合格数量。" /> : p.receipts.map(r => <div className="commerce-record" key={r.id}><div className="inline"><strong>到货 #{r.id}</strong><span>{r.warehouse_name}</span><span className="grow" /><span className="mono t3">{fmtDate(r.received_at)}</span></div><div className="table-wrap"><table className="table"><thead><tr><th>物料</th><th className="num">实收</th><th className="num">不合格</th><th>等级 / 合格数</th><th>入库批次</th></tr></thead><tbody>{r.lines.map(l => <tr key={l.line_id}><td className="wrap">{lineName(l.line_id)}</td><td className="num mono">{l.qty}</td><td className="num mono">{l.rejected}</td><td className="wrap">{l.grades.map(g => `${g.grade || '无等级'} × ${g.qty}`).join(' · ') || '—'}</td><td className="mono t3">{l.batch_ids.map(b => `#${b}`).join(' / ') || '—'}</td></tr>)}</tbody></table></div>{r.note && <div className="help">{r.note}</div>}</div>)}</DocumentPanel>
        <DocumentPanel title={`不合格退货 · ${p.returns.filter(r => r.status === 'pending').length} 笔待退`}>{!p.returns.length ? <Empty title="没有不合格退货" /> : <div className="table-wrap"><table className="table"><thead><tr><th>物料</th><th className="num">数量</th><th className="num">退货金额</th><th>状态</th><th /></tr></thead><tbody>{p.returns.map(r => <tr key={r.id}><td className="wrap">{lineName(r.line_id)}</td><td className="num mono">{r.qty}</td><td className="num mono">{money(r.amount, p.currency)}</td><td><Pill kind={r.status === 'pending' ? 'pill-warning' : 'pill-neutral'}>{r.status === 'pending' ? '待退' : '已退'}</Pill></td><td>{r.status === 'pending' && <button className="btn btn-secondary btn-sm" disabled={busy} onClick={() => setDrawer(`return:${r.id}`)}>登记已退</button>}</td></tr>)}</tbody></table></div>}</DocumentPanel>
      </div>
      <aside className="stack"><DocumentPanel title="采购信息"><dl className="kv commerce-kv"><dt>供应商</dt><dd><Link className="link" to="/suppliers">{p.supplier_name}</Link></dd><dt>类型</dt><dd>{p.is_sample ? '拍样单' : '普通采购'}</dd>{p.product_id && <><dt>关联商品</dt><dd><Link className="link" to={`/products/${p.product_id}`}>商品 #{p.product_id}</Link></dd></>}<dt>预计到货</dt><dd className="mono">{p.expected_at ? fmtDate(p.expected_at) : '未填写'}</dd><dt>运费</dt><dd className="mono">{money(p.shipping_fee, p.currency)}</dd><dt>创建</dt><dd className="mono">{fmtDate(p.created_at, true)}</dd>{p.ordered_at && <><dt>下单</dt><dd className="mono">{fmtDate(p.ordered_at, true)}</dd></>}{p.done_at && <><dt>{p.status === 'canceled' ? '取消' : '完成'}</dt><dd className="mono">{fmtDate(p.done_at, true)}</dd></>}<dt>备注</dt><dd className="commerce-pre">{p.note || '—'}</dd></dl></DocumentPanel></aside>
    </main></div>
    {drawer === 'edit' && <PurchaseForm value={p} onClose={() => setDrawer('')} onDone={() => { setDrawer(''); reload() }} />}
    {drawer === 'receipt' && <ReceiptForm value={p} onClose={() => setDrawer('')} onDone={() => { toast('到货质检已登记', 'ok'); setDrawer(''); reload() }} />}
    {drawer === 'note' && <PurchaseNote value={p} onClose={() => setDrawer('')} onDone={() => { setDrawer(''); reload() }} />}
    {['order', 'finish', 'cancel'].includes(drawer) && <Drawer title={{ order: '确认下单', finish: '完成采购', cancel: '取消采购' }[drawer]} onClose={() => { if (!busy) setDrawer('') }} foot={<><button className="btn btn-secondary" disabled={busy} onClick={() => setDrawer('')}>返回</button><button className="btn btn-primary" disabled={busy} onClick={() => action(`/api/purchase-orders/${p.id}/${drawer}`, '采购状态已更新')}>确认</button></>}><p>{drawer === 'order' ? '下单后将锁定供应商、金额和明细，可分批登记到货。' : drawer === 'finish' ? '完成后停止登记到货；尚未到货的剩余数量不再采购。待退单仍可继续登记。' : '取消后不再收货，本采购单会保留供查询。'}</p></Drawer>}
    {drawer.startsWith('return:') && <Drawer title="登记已退给供应商" onClose={() => { if (!busy) setDrawer('') }} foot={<><button className="btn btn-secondary" disabled={busy} onClick={() => setDrawer('')}>返回</button><button className="btn btn-primary" disabled={busy} onClick={() => action(`/api/purchase-returns/${drawer.split(':')[1]}`, '已登记退货完成', { status: 'done' })}>确认已退</button></>}><p>请确认不合格品已退给供应商。登记后会保留退货金额与记录。</p></Drawer>}
  </>
}

function PurchaseNote({ value, onClose, onDone }: { value: Purchase; onClose: () => void; onDone: () => void }) {
  const [date, setDate] = useState(value.expected_at.slice(0, 10))
  const [note, setNote] = useState(value.note)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const save = async () => { setBusy(true); try { await api(`/api/purchase-orders/${value.id}`, { method: 'PATCH', body: { expected_at: date, note } }); onDone() } catch (e) { setError(errText(e)) } finally { setBusy(false) } }
  return <Drawer title="修改采购备注与日期" onClose={() => { if (!busy) onClose() }} foot={<><button className="btn btn-secondary" disabled={busy} onClick={onClose}>取消</button><button className="btn btn-primary" disabled={busy} onClick={save}>保存</button></>}><Field label="预计到货"><input className="input mono" type="date" value={date} onChange={e => setDate(e.target.value)} /></Field><Field label="备注"><textarea className="textarea" rows={4} maxLength={1000} value={note} onChange={e => setNote(e.target.value)} /></Field>{error && <div className="form-err" role="alert">{error}</div>}</Drawer>
}
