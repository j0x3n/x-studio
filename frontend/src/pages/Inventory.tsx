import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { Plus, Search, ArrowDownToLine, ArrowRightLeft, Download, ClipboardList, Pencil, AlertTriangle } from 'lucide-react'
import { api, qs, type Page } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { useCategories } from '../lib/categories'
import { fmtDate, fmtAgo } from '../lib/format'
import { kindName, whTypeName, ledgerName, countStatus, yuan, specText, availText, type InvItem, type Ledger, type Count, type Warehouse, type ItemKind, type LedgerKind } from '../lib/inventory'
import { Topbar, Loading, Empty, Tabs, Pill, Drawer, Field } from '../components/ui'
import { MoveDrawer, TransferDrawer, ItemDrawer, WarehouseDrawer, useWarehouses, useCostPerm } from '../components/InventoryForms'

type Tab = 'items' | 'ledger' | 'counts' | 'warehouses'

export function Inventory() {
  const { can } = useAuth()
  const edit = can('inventory.edit')
  const [sp, setSp] = useSearchParams()
  const tab = (sp.get('tab') as Tab) || 'items'
  const setTab = (t: Tab) => { const n = new URLSearchParams(); if (t !== 'items') n.set('tab', t); setSp(n, { replace: true }) }
  const [drawer, setDrawer] = useState<'' | 'move' | 'transfer' | 'item' | 'count'>('')
  const [stamp, setStamp] = useState(0)
  const done = () => { setDrawer(''); setStamp(s => s + 1) }

  return (
    <>
      <Topbar title="库存" sub="全公司一份库存，所有店铺共用">
        <a className="btn btn-ghost" href="/api/inventory/export.csv?kind=sku" title="导出成品可售数，给千牛批量改库存"><Download />导出可售数</a>
        {edit && <button className="btn btn-secondary" onClick={() => setDrawer('transfer')}><ArrowRightLeft />调拨</button>}
        {edit && <button className="btn btn-secondary" onClick={() => setDrawer('move')}><ArrowDownToLine />出入库</button>}
        {edit && <button className="btn btn-primary" onClick={() => setDrawer('item')}><Plus />新建物料</button>}
      </Topbar>
      <div className="scroll"><main className="content">
        <Tabs value={tab} onChange={setTab} items={[{ key: 'items', label: '物料' }, { key: 'ledger', label: '流水' }, { key: 'counts', label: '盘点' }, { key: 'warehouses', label: '仓库' }]} />
        {tab === 'items' && <ItemsTab key={stamp} />}
        {tab === 'ledger' && <LedgerTab key={stamp} />}
        {tab === 'counts' && <CountsTab key={stamp} onNew={() => setDrawer('count')} />}
        {tab === 'warehouses' && <WarehousesTab key={stamp} />}
      </main></div>
      {drawer === 'move' && <MoveDrawer onClose={() => setDrawer('')} onDone={done} />}
      {drawer === 'transfer' && <TransferDrawer onClose={() => setDrawer('')} onDone={done} />}
      {drawer === 'item' && <ItemDrawer onClose={() => setDrawer('')} onDone={done} />}
      {drawer === 'count' && <NewCountDrawer onClose={() => setDrawer('')} />}
    </>
  )
}

function ItemsTab() {
  const nav = useNavigate()
  const costPerm = useCostPerm()
  const { items: cats } = useCategories()
  const { items: whs } = useWarehouses()
  const [kind, setKind] = useState<ItemKind | ''>('')
  const [cat, setCat] = useState('')
  const [wh, setWh] = useState('')
  const [low, setLow] = useState(false)
  const [archived, setArchived] = useState(false)
  const [q, setQ] = useState('')
  const [data, setData] = useState<Page<InvItem> | null>(null)
  const load = useCallback(() => {
    api<Page<InvItem>>('/api/inventory/items' + qs({ q, kind, category_id: cat, warehouse_id: wh, low: low ? 1 : undefined, archived: archived ? 1 : undefined, page_size: 200 }))
      .then(setData).catch(() => setData({ items: [], total: 0 }))
  }, [q, kind, cat, wh, low, archived])
  useEffect(() => { const t = setTimeout(load, q ? 250 : 0); return () => clearTimeout(t) }, [load, q])
  const items = data?.items || []
  const lowN = items.filter(i => i.low).length
  const value = items.reduce((s, i) => s + (i.cost_value || 0), 0)
  return (
    <>
      <div className="toolbar">
        <div className="segmented">{([['', '全部'], ['sku', '成品'], ['material', '原料配件'], ['packaging', '包装耗材']] as [ItemKind | '', string][]).map(([k, l]) => <button key={k} className={kind === k ? 'active' : ''} onClick={() => setKind(k)}>{l}</button>)}</div>
        <select className="input" style={{ width: 110 }} value={cat} onChange={e => setCat(e.target.value)}><option value="">全部品类</option>{(cats || []).map(c => <option key={c.id} value={c.id}>{c.name}</option>)}</select>
        <select className="input" style={{ width: 130 }} value={wh} onChange={e => setWh(e.target.value)}><option value="">全部仓库</option>{(whs || []).filter(w => w.type !== 'dropship').map(w => <option key={w.id} value={w.id}>{w.name}</option>)}</select>
        <button className={'btn btn-sm ' + (low ? 'btn-soft' : 'btn-ghost')} onClick={() => setLow(!low)}><AlertTriangle />低于安全库存</button>
        <label className="inline fs-12"><input type="checkbox" className="check" checked={archived} onChange={e => setArchived(e.target.checked)} />含归档</label>
        <span className="spacer" />
        <label className="field" style={{ width: 220 }}><Search /><input placeholder="编码或名称" value={q} onChange={e => setQ(e.target.value)} /></label>
      </div>
      <section className="panel">
        {!data ? <Loading /> : items.length === 0 ? <Empty title={q || kind || cat || low ? '没有符合条件的物料' : '还没有物料'} desc={q || kind || cat || low ? undefined : '先「新建物料」，再用「出入库 → 手工入库」录期初库存。'} /> : (
          <div className="table-wrap"><table className="table">
            <thead><tr><th>物料</th><th>类型</th><th>挂在商品</th><th className="num">现有</th><th className="num">占用</th><th className="num">可售</th><th className="num">安全库存</th>{costPerm && <th className="num">库存成本</th>}<th>更新</th></tr></thead>
            <tbody>{items.map(it => (
              <tr key={it.id} className="clickable" onClick={() => nav(`/inventory/items/${it.id}`)}>
                <td className="wrap"><div className="cell-title"><span className="mono">{it.code}</span> {it.name}{it.archived && <span className="tag tag-gray" style={{ marginLeft: 6 }}>已归档</span>}</div>{Object.keys(it.spec || {}).length > 0 && <div className="cell-sub">{specText(it.spec)}</div>}</td>
                <td className="t2">{kindName[it.kind]}</td>
                <td className="ellipsis" style={{ maxWidth: 180 }}>{it.product ? (it.product.name ? <Link className="link" to={`/products/${it.product.id}`} onClick={e => e.stopPropagation()}>{it.product.name}</Link> : <span className="t4">无权查看</span>) : <span className="t4">—</span>}</td>
                <td className="num mono">{it.on_hand}{it.dropship > 0 && <span className="t3"> +{it.dropship}代发</span>}</td>
                <td className="num mono">{it.reserved || <span className="t4">—</span>}</td>
                <td className={'num mono' + (it.low ? ' danger-text' : '')}>{availText(it)}{it.low && <AlertTriangle className="inv-low-ico" />}</td>
                <td className="num mono t3">{it.safety_stock || '—'}</td>
                {costPerm && <td className="num mono">{yuan(it.cost_value)}</td>}
                <td className="t3">{fmtAgo(it.updated_at)}</td>
              </tr>
            ))}</tbody>
          </table></div>
        )}
        {data && items.length > 0 && <div className="pager">{data.total} 种物料{lowN > 0 && <>，<span className="danger-text">{lowN} 种低于安全库存</span></>}{costPerm && <>，库存成本合计 {yuan(value)}</>}{data.total > items.length && `（只显示前 ${items.length} 种）`}</div>}
      </section>
    </>
  )
}

export function LedgerTable({ rows, showItem = true }: { rows: Ledger[]; showItem?: boolean }) {
  const costPerm = useCostPerm()
  return (
    <div className="table-wrap"><table className="table">
      <thead><tr><th>时间</th>{showItem && <th>物料</th>}<th>类型</th><th>仓库</th><th className="num">数量</th>{costPerm && <th className="num">成本</th>}<th>批次</th><th>来源</th><th>操作人</th><th>备注</th></tr></thead>
      <tbody>{rows.map(l => (
        <tr key={l.id}>
          <td className="t3 nowrap">{fmtDate(l.created_at, true)}</td>
          {showItem && <td className="wrap"><Link className="link" to={`/inventory/items/${l.item_id}`}><span className="mono">{l.code}</span> {l.item_name || l.name}</Link></td>}
          <td>{ledgerName[l.kind] || l.kind}</td>
          <td className="t2">{l.warehouse_name}</td>
          <td className={'num mono ' + (l.qty > 0 ? 'inv-in' : 'inv-out')}>{l.qty > 0 ? '+' : ''}{l.qty}</td>
          {costPerm && <td className="num mono t2">{yuan(l.cost)}</td>}
          <td className="mono t3">{l.batch_id ? `#${l.batch_id}` : '—'}</td>
          <td>{l.ref_path ? <Link className="link" to={l.ref_path}>{l.ref_label || l.ref_type}</Link> : <span className="t3">{l.ref_label || (l.ref_type === 'manual' ? '手工' : l.ref_type)}</span>}</td>
          <td className="t2">{l.user_name}</td>
          <td className="t3 ellipsis" style={{ maxWidth: 200 }} title={l.note}>{l.note}</td>
        </tr>
      ))}</tbody>
    </table></div>
  )
}

function LedgerTab() {
  const { items: whs } = useWarehouses(true)
  const [kind, setKind] = useState('')
  const [wh, setWh] = useState('')
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  const [page, setPage] = useState(1)
  const [data, setData] = useState<Page<Ledger> | null>(null)
  useEffect(() => {
    api<Page<Ledger>>('/api/inventory/ledger' + qs({ kind, warehouse_id: wh, from, to, page, page_size: 100 })).then(setData).catch(() => setData({ items: [], total: 0 }))
  }, [kind, wh, from, to, page])
  const pages = data ? Math.max(1, Math.ceil(data.total / 100)) : 1
  return (
    <>
      <div className="toolbar">
        <select className="input" style={{ width: 120 }} value={kind} onChange={e => { setKind(e.target.value); setPage(1) }}><option value="">全部类型</option>{(Object.keys(ledgerName) as LedgerKind[]).filter(k => !k.startsWith('assemble')).map(k => <option key={k} value={k}>{ledgerName[k]}</option>)}</select>
        <select className="input" style={{ width: 130 }} value={wh} onChange={e => { setWh(e.target.value); setPage(1) }}><option value="">全部仓库</option>{(whs || []).map(w => <option key={w.id} value={w.id}>{w.name}</option>)}</select>
        <input className="input" type="date" style={{ width: 140 }} value={from} onChange={e => { setFrom(e.target.value); setPage(1) }} />
        <span className="t3">到</span>
        <input className="input" type="date" style={{ width: 140 }} value={to} onChange={e => { setTo(e.target.value); setPage(1) }} />
      </div>
      <section className="panel">
        {!data ? <Loading /> : data.items.length === 0 ? <Empty title="没有流水" /> : <LedgerTable rows={data.items} />}
        {data && data.total > 100 && (
          <div className="pager">共 {data.total} 条<span className="spacer" />
            <button className="btn btn-ghost btn-sm" disabled={page <= 1} onClick={() => setPage(p => p - 1)}>上一页</button>
            <span className="mono">{page} / {pages}</span>
            <button className="btn btn-ghost btn-sm" disabled={page >= pages} onClick={() => setPage(p => p + 1)}>下一页</button>
          </div>
        )}
      </section>
    </>
  )
}

function CountsTab({ onNew }: { onNew: () => void }) {
  const nav = useNavigate()
  const { can } = useAuth()
  const [data, setData] = useState<Page<Count> | null>(null)
  useEffect(() => { api<Page<Count>>('/api/inventory/counts' + qs({ page_size: 100 })).then(setData).catch(() => setData({ items: [], total: 0 })) }, [])
  return (
    <section className="panel">
      <div className="panel-head"><div className="titles"><div className="panel-title">盘点单</div><div className="panel-sub">按「实盘数 − 当前现有数」生成盘盈盘亏流水，盘点期间照常出入库也对得上</div></div><span className="grow" />{can('inventory.edit') && <button className="btn btn-secondary btn-sm" onClick={onNew}><ClipboardList />开始盘点</button>}</div>
      {!data ? <Loading /> : data.items.length === 0 ? <Empty title="还没有盘点过" /> : (
        <div className="table-wrap"><table className="table">
          <thead><tr><th>单号</th><th>仓库</th><th>状态</th><th className="num">已盘 / 物料数</th><th>备注</th><th>开始</th><th>完成</th><th>操作人</th></tr></thead>
          <tbody>{data.items.map(c => (
            <tr key={c.id} className="clickable" onClick={() => nav(`/inventory/counts/${c.id}`)}>
              <td className="mono">#{c.id}</td><td>{c.warehouse_name}</td><td><Pill kind={countStatus[c.status].pill}>{countStatus[c.status].label}</Pill></td>
              <td className="num mono">{c.line_count == null ? '—' : `${c.counted_count ?? 0} / ${c.line_count}`}</td><td className="t2 ellipsis" style={{ maxWidth: 200 }}>{c.note}</td>
              <td className="t3">{fmtDate(c.created_at, true)}</td><td className="t3">{c.done_at ? fmtDate(c.done_at, true) : '—'}</td><td className="t2">{c.created_by_name}</td>
            </tr>
          ))}</tbody>
        </table></div>
      )}
    </section>
  )
}

function NewCountDrawer({ onClose }: { onClose: () => void }) {
  const nav = useNavigate()
  const toast = useToast()
  const { items: whs } = useWarehouses()
  const stockWhs = (whs || []).filter(w => w.type !== 'dropship')
  const [wh, setWh] = useState(0)
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (!wh && stockWhs[0]) setWh(stockWhs[0].id) }, [stockWhs, wh])
  const submit = async () => {
    setBusy(true)
    try {
      const r = await api<{ id?: number; count?: { id: number } }>('/api/inventory/counts', { body: { warehouse_id: wh, note } })
      nav(`/inventory/counts/${r.count?.id ?? r.id}`)
    } catch (e) { toast(errText(e), 'error'); setBusy(false) }
  }
  return (
    <Drawer title="开始盘点" width={400} onClose={onClose}
      foot={<><span className="grow" /><button className="btn btn-secondary" onClick={onClose}>取消</button><button className="btn btn-primary" disabled={busy || !wh} onClick={submit}>开始</button></>}>
      <Field label="盘哪个仓" hint="会把这个仓现在有库存的物料都列进来，盘点单里还可以再加"><select className="input" value={wh} onChange={e => setWh(Number(e.target.value))}>{stockWhs.map(w => <option key={w.id} value={w.id}>{w.name}</option>)}</select></Field>
      <Field label="备注"><input className="input" value={note} onChange={e => setNote(e.target.value)} placeholder="如 10 月月底盘点" /></Field>
    </Drawer>
  )
}

function WarehousesTab() {
  const { can } = useAuth()
  const costPerm = useCostPerm()
  const { items, reload } = useWarehouses(true)
  const [edit, setEdit] = useState<Warehouse | null | undefined>(undefined)
  return (
    <section className="panel">
      <div className="panel-head"><div className="titles"><div className="panel-title">仓库</div><div className="panel-sub">可售数 = 自有仓 + 档口的现有数 + 代发可售数 − 订单占用</div></div><span className="grow" />{can('inventory.edit') && <button className="btn btn-secondary btn-sm" onClick={() => setEdit(null)}><Plus />新建仓库</button>}</div>
      {!items ? <Loading /> : (
        <div className="table-wrap"><table className="table">
          <thead><tr><th>名称</th><th>类型</th><th className="num">有库存的物料</th>{costPerm && <th className="num">库存成本</th>}<th>地址</th><th>备注</th><th /></tr></thead>
          <tbody>{items.map(w => (
            <tr key={w.id} className={w.archived ? 't4' : ''}>
              <td className="cell-title">{w.name}{w.archived && <span className="tag tag-gray" style={{ marginLeft: 6 }}>已归档</span>}</td>
              <td><span className={'tag ' + (w.type === 'dropship' ? 'tag-purple' : w.type === 'stall' ? 'tag-orange' : 'tag-blue')}>{whTypeName[w.type]}</span></td>
              <td className="num mono">{w.item_count ?? '—'}</td>
              {costPerm && <td className="num mono">{w.type === 'dropship' ? <span className="t4">不算资产</span> : yuan(w.cost_value)}</td>}
              <td className="t2 ellipsis" style={{ maxWidth: 220 }}>{w.address}</td>
              <td className="t3 ellipsis" style={{ maxWidth: 200 }}>{w.note}</td>
              <td className="num">{can('inventory.edit') && <button className="btn btn-ghost btn-icon btn-sm" onClick={() => setEdit(w)}><Pencil /></button>}</td>
            </tr>
          ))}</tbody>
        </table></div>
      )}
      {edit !== undefined && <WarehouseDrawer wh={edit} onClose={() => setEdit(undefined)} onDone={() => { setEdit(undefined); reload() }} />}
    </section>
  )
}
