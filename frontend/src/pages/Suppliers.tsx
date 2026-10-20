import { useState } from 'react'
import { Link } from 'react-router-dom'
import { Plus, Search, Pencil } from 'lucide-react'
import { api, qs, type Page } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { fmtAgo } from '../lib/format'
import type { Supplier } from '../lib/commerce'
import { Topbar, Empty, Drawer, Field } from '../components/ui'
import { Pager, RecordState, useRecord } from '../components/CommerceShared'

export function Suppliers() {
  const { can } = useAuth()
  const edit = can('purchase.manage')
  const [q, setQ] = useState('')
  const [archived, setArchived] = useState(false)
  const [page, setPage] = useState(1)
  const [selected, setSelected] = useState<Supplier | null | undefined>(undefined)
  const { data, error, reload } = useRecord<Page<Supplier>>('/api/suppliers' + qs({ q, archived: archived ? 1 : undefined, page, page_size: 50 }))
  if (!edit && !can('inventory.view') && !can('finance.view')) return <><Topbar title="供应商" /><Empty title="没有查看供应商的权限" /></>
  return <>
    <Topbar title="供应商" sub="货源、联系人与结算方式">{edit && <button className="btn btn-primary" onClick={() => setSelected(null)}><Plus />新建供应商</button>}</Topbar>
    <div className="scroll"><main className="content">
      <div className="toolbar"><label className="field commerce-search"><Search /><input placeholder="供应商、联系人或主营" value={q} onChange={e => { setQ(e.target.value); setPage(1) }} /></label><label className="inline fs-12"><input className="check" type="checkbox" checked={archived} onChange={e => { setArchived(e.target.checked); setPage(1) }} />含归档</label></div>
      <section className="panel">
        {!data ? <RecordState error={error} reload={reload} /> : !data.items.length ? <Empty title={q ? '没有符合条件的供应商' : '还没有供应商'} desc="采购前先记录供应商，也可以在这里维护历史货源。" /> : <div className="table-wrap"><table className="table"><thead><tr><th>供应商</th><th>联系人</th><th>电话 / 微信</th><th>主营</th><th>结算</th><th>更新</th><th /></tr></thead><tbody>{data.items.map(s => <tr key={s.id}>
          <td className="wrap"><div className="cell-title">{s.name} {s.archived && <span className="tag tag-gray">已归档</span>}</div><div className="cell-sub">{s.address || '未填地址'}</div></td><td>{s.contact || '—'}</td><td><div className="mono">{s.phone || '—'}</div>{s.wechat && <div className="cell-sub">微信 {s.wechat}</div>}</td><td className="wrap t2">{s.products || '—'}</td><td>{s.settlement === 'monthly' ? `月结 · ${s.payment_days} 天` : '现结'}</td><td className="t3">{fmtAgo(s.updated_at)}</td><td><div className="inline">{edit && <><Link className="btn btn-ghost btn-sm" to={`/purchase-orders?supplier_id=${s.id}`}>采购记录</Link><button className="btn btn-ghost btn-icon btn-sm" title={`编辑${s.name}`} onClick={() => setSelected(s)}><Pencil /></button></>}</div></td>
        </tr>)}</tbody></table></div>}
        {data && <Pager total={data.total} page={page} setPage={setPage} />}
      </section>
    </main></div>
    {selected !== undefined && <SupplierDrawer value={selected} onClose={() => setSelected(undefined)} onDone={() => { setSelected(undefined); reload() }} />}
  </>
}

function SupplierDrawer({ value, onClose, onDone }: { value: Supplier | null; onClose: () => void; onDone: () => void }) {
  const toast = useToast()
  const [form, setForm] = useState({ name: value?.name || '', contact: value?.contact || '', phone: value?.phone || '', wechat: value?.wechat || '', address: value?.address || '', products: value?.products || '', settlement: value?.settlement || 'cash', payment_days: value?.payment_days || 0, note: value?.note || '', archived: value?.archived || false })
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const text = (key: 'name' | 'contact' | 'phone' | 'wechat' | 'address' | 'products', label: string, max = 200) => <Field label={label} req={key === 'name'}><input className="input" maxLength={max} value={form[key]} onChange={e => setForm({ ...form, [key]: e.target.value })} /></Field>
  const save = async () => {
    setBusy(true); setError('')
    try { await api('/api/suppliers' + (value ? `/${value.id}` : ''), { method: value ? 'PATCH' : 'POST', body: form }); toast('供应商已保存', 'ok'); onDone() }
    catch (e) { setError(errText(e)) } finally { setBusy(false) }
  }
  return <Drawer title={value ? '编辑供应商' : '新建供应商'} width={480} onClose={() => { if (!busy) onClose() }} foot={<><button className="btn btn-secondary" disabled={busy} onClick={onClose}>取消</button><button className="btn btn-primary" disabled={busy || !form.name.trim()} onClick={save}>{busy ? '保存中…' : '保存供应商'}</button></>}>
    {text('name', '供应商名称', 50)}<div className="form-row">{text('contact', '联系人')}{text('phone', '电话')}</div>{text('wechat', '微信')}{text('address', '地址')}{text('products', '主营')}
    <div className="form-row"><Field label="结算方式"><select className="input" value={form.settlement} onChange={e => setForm({ ...form, settlement: e.target.value as Supplier['settlement'] })}><option value="cash">现结</option><option value="monthly">月结</option></select></Field><Field label="账期（天）"><input className="input mono" type="number" min={0} max={365} step={1} value={form.payment_days} onChange={e => setForm({ ...form, payment_days: Number(e.target.value) })} /></Field></div>
    <Field label="备注"><textarea className="textarea" rows={3} maxLength={1000} value={form.note} onChange={e => setForm({ ...form, note: e.target.value })} /></Field>
    {value && <label className="inline"><input type="checkbox" className="check" checked={form.archived} onChange={e => setForm({ ...form, archived: e.target.checked })} />归档供应商<span className="t3 fs-12">归档后不能新建采购单</span></label>}
    {error && <div className="form-err" role="alert">{error}</div>}
  </Drawer>
}
