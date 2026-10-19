import { useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { Plus, Search } from 'lucide-react'
import { qs, type Page } from '../lib/api'
import { useAuth } from '../lib/auth'
import { fmtDate } from '../lib/format'
import { money, purchaseStatus, type Purchase, type Supplier } from '../lib/commerce'
import { Topbar, Empty, Pill, Tabs } from '../components/ui'
import { Pager, RecordState, useRecord } from '../components/CommerceShared'
import { PurchaseForm } from '../components/PurchaseForms'

export function Purchases() {
  const { can } = useAuth()
  const nav = useNavigate()
  const [sp, setSp] = useSearchParams()
  const status = sp.get('status') || ''
  const supplier = sp.get('supplier_id') || ''
  const sample = sp.get('is_sample') || ''
  const q = sp.get('q') || ''
  const page = Number(sp.get('page')) || 1
  const set = (key: string, value: string) => { const n = new URLSearchParams(sp); if (value) n.set(key, value); else n.delete(key); if (key !== 'page') n.delete('page'); setSp(n, { replace: true }) }
  const [create, setCreate] = useState(false)
  const { data, error, reload } = useRecord<Page<Purchase>>('/api/purchase-orders' + qs({ status, supplier_id: supplier, is_sample: sample, q, page, page_size: 50 }))
  const { data: suppliers } = useRecord<Page<Supplier>>('/api/suppliers?archived=1&page_size=200')
  if (!can('purchase.manage')) return <><Topbar title="采购单" /><Empty title="没有管理采购的权限" /></>
  return <>
    <Topbar title="采购单" sub="从下单、到货质检到入库"><Link className="btn btn-secondary" to="/suppliers">供应商</Link><button className="btn btn-primary" onClick={() => setCreate(true)}><Plus />新建采购单</button></Topbar>
    <div className="scroll"><main className="content">
      <Tabs value={status} onChange={v => set('status', v)} items={[{ key: '', label: '全部' }, ...Object.entries(purchaseStatus).map(([key, v]) => ({ key, label: v.label }))]} />
      <div className="toolbar"><select className="input commerce-select" aria-label="供应商筛选" value={supplier} onChange={e => set('supplier_id', e.target.value)}><option value="">全部供应商</option>{suppliers?.items.map(s => <option key={s.id} value={s.id}>{s.name}</option>)}</select><select className="input commerce-select" aria-label="采购类型" value={sample} onChange={e => set('is_sample', e.target.value)}><option value="">全部采购</option><option value="1">拍样单</option><option value="0">普通采购</option></select><span className="spacer" /><label className="field commerce-search"><Search /><input placeholder="搜索采购单号" value={q} onChange={e => set('q', e.target.value)} /></label></div>
      <section className="panel">
        {!data ? <RecordState error={error} reload={reload} /> : !data.items.length ? <Empty title={status || q || supplier || sample ? '没有符合条件的采购单' : '还没有采购单'} desc="新建采购单后，按实际到货分批质检入库。" /> : <div className="table-wrap"><table className="table"><thead><tr><th>采购单</th><th>供应商</th><th>状态</th><th>预计到货</th><th className="num">货款</th><th className="num">运费</th><th className="num">合计</th><th>创建时间</th></tr></thead><tbody>{data.items.map(p => <tr className="clickable" key={p.id} onClick={() => nav(`/purchase-orders/${p.id}`)}><td><Link className="mono link" to={`/purchase-orders/${p.id}`}>{p.no}</Link>{p.is_sample && <span className="tag tag-purple commerce-tag">拍样</span>}<div className="cell-sub commerce-ellipsis">{p.note}</div></td><td>{p.supplier_name}</td><td><Pill kind={purchaseStatus[p.status].pill}>{purchaseStatus[p.status].label}</Pill></td><td className="mono t2">{p.expected_at ? fmtDate(p.expected_at) : '—'}</td><td className="num mono">{money(p.goods_amount, p.currency)}</td><td className="num mono t2">{money(p.shipping_fee, p.currency)}</td><td className="num mono">{money((p.goods_amount || 0) + p.shipping_fee, p.currency)}</td><td className="mono t3">{fmtDate(p.created_at)}</td></tr>)}</tbody></table></div>}
        {data && <Pager total={data.total} page={page} setPage={n => set('page', String(n))} />}
      </section>
    </main></div>
    {create && <PurchaseForm supplierID={Number(supplier)} onClose={() => setCreate(false)} onDone={id => nav(`/purchase-orders/${id}`)} />}
  </>
}
