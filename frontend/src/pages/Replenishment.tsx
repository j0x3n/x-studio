import { useState, useMemo } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { Search, ShoppingCart, RefreshCw, Plus, CheckSquare, Square, AlertCircle, FileText } from 'lucide-react'
import { api, qs, type Page } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { toFen, fenToInput } from '../lib/inventory'
import { money, type Purchase, type Supplier } from '../lib/commerce'
import { Topbar, Empty, Drawer, Field } from '../components/ui'
import { Pager, RecordState, useRecord } from '../components/CommerceShared'
import '../styles/intake.css'

export type ReplenishmentItem = {
  item_id: number
  code: string
  name: string
  unit: string
  available: number
  safety_stock: number
  incoming: number
  suggested_qty: number
  supplier_id?: number | null
  supplier_name?: string | null
  cost?: number | null
  currency?: string
}

export function Replenishment() {
  const { can } = useAuth()
  const nav = useNavigate()
  const [sp, setSp] = useSearchParams()

  const q = sp.get('q') || ''
  const target = sp.get('target') || ''
  const showAll = sp.get('all') === '1'
  const page = Number(sp.get('page')) || 1

  const setParam = (key: string, value: string) => {
    const n = new URLSearchParams(sp)
    if (value) n.set(key, value)
    else n.delete(key)
    if (key !== 'page') n.delete('page')
    setSp(n, { replace: true })
  }

  const { data, error, reload } = useRecord<Page<ReplenishmentItem>>(
    '/api/inventory/replenishment' + qs({ q, target: target || undefined, all: showAll ? '1' : undefined, page, page_size: 50 })
  )

  const canPurchase = can('purchase.manage')
  const canSeeCost = can('purchase.manage') || can('product.cost')

  // 多选状态
  const [selected, setSelected] = useState<Record<number, ReplenishmentItem>>({})
  const [draftDrawer, setDraftDrawer] = useState(false)

  const selectedItems = useMemo(() => Object.values(selected), [selected])
  const selectedCount = selectedItems.length

  const allPageSelected = useMemo(() => {
    if (!data || !data.items.length) return false
    return data.items.every(it => selected[it.item_id])
  }, [data, selected])

  const toggleSelectAll = () => {
    if (!data?.items.length) return
    if (allPageSelected) {
      setSelected(prev => {
        const next = { ...prev }
        for (const it of data.items) delete next[it.item_id]
        return next
      })
    } else {
      setSelected(prev => {
        const next = { ...prev }
        for (const it of data.items) next[it.item_id] = it
        return next
      })
    }
  }

  const toggleItem = (it: ReplenishmentItem) => {
    setSelected(prev => {
      const next = { ...prev }
      if (next[it.item_id]) delete next[it.item_id]
      else next[it.item_id] = it
      return next
    })
  }

  // 检查已选物料的供应商情况
  const supplierGrouping = useMemo(() => {
    const map = new Map<number, string>()
    let unassigned = 0
    for (const it of selectedItems) {
      if (it.supplier_id && it.supplier_id > 0) {
        map.set(it.supplier_id, it.supplier_name || `供应商 #${it.supplier_id}`)
      } else {
        unassigned++
      }
    }
    return {
      supplierCount: map.size,
      suppliers: Array.from(map.entries()).map(([id, name]) => ({ id, name })),
      unassigned,
      isUniform: map.size === 1 && unassigned === 0,
      singleSupplierID: map.size === 1 ? Array.from(map.keys())[0] : 0,
      singleSupplierName: map.size === 1 ? Array.from(map.values())[0] : '',
    }
  }, [selectedItems])

  if (!can('inventory.view')) {
    return (
      <>
        <Topbar title="补货建议" />
        <Empty title="没有查看库存的权限" desc="请联系管理员开通 inventory.view 权限。" />
      </>
    )
  }

  return (
    <>
      <Topbar title="补货建议" sub="结合现有库存、已占用缺口、安全库存及在途采购，动态计算补货建议">
        {canPurchase && (
          <>
            <Link className="btn btn-secondary" to="/purchase-recognize">
              <FileText />
              单据识别
            </Link>
            <Link className="btn btn-secondary" to="/purchase-orders">
              <ShoppingCart />
              采购单
            </Link>
          </>
        )}
      </Topbar>

      <div className="scroll">
        <main className="content">
          <div className="toolbar replenishment-toolbar">
            <div className="tabs">
              <button
                className={`tab ${!showAll ? 'active' : ''}`}
                onClick={() => setParam('all', '')}
              >
                需补货物料
              </button>
              <button
                className={`tab ${showAll ? 'active' : ''}`}
                onClick={() => setParam('all', '1')}
              >
                全部物料
              </button>
            </div>

            <span className="spacer" />

            <div className="replenishment-target" title="按指定的目标库存基数统一计算建议量">
              <span>目标库存：</span>
              <input
                className="input mono"
                type="number"
                min="0"
                placeholder="安全库存"
                value={target}
                onChange={e => setParam('target', e.target.value)}
              />
            </div>

            <label className="field commerce-search">
              <Search />
              <input
                placeholder="搜索物料编码或名称"
                value={q}
                onChange={e => setParam('q', e.target.value)}
              />
            </label>

            <button className="btn btn-secondary btn-icon" title="刷新列表" onClick={reload}>
              <RefreshCw />
            </button>
          </div>

          <section className="panel">
            {!data ? (
              <RecordState error={error} reload={reload} />
            ) : !data.items.length ? (
              <Empty
                title={q || target || showAll ? '没有符合条件的物料' : '当前没有需要补货的物料'}
                desc={showAll ? '未找到相关物料记录。' : '现有可用数及在途数量均满足安全库存要求。'}
              />
            ) : (
              <div className="table-wrap">
                <table className="table">
                  <thead>
                    <tr>
                      {canPurchase && (
                        <th style={{ width: 38 }}>
                          <button
                            className="btn btn-ghost btn-icon btn-sm"
                            title={allPageSelected ? '取消全选本页' : '全选本页'}
                            onClick={toggleSelectAll}
                          >
                            {allPageSelected ? <CheckSquare /> : <Square />}
                          </button>
                        </th>
                      )}
                      <th>物料</th>
                      <th className="num">可用数</th>
                      <th className="num">安全库存</th>
                      <th className="num">在途采购</th>
                      <th className="num">建议补货</th>
                      {canSeeCost && (
                        <>
                          <th>推荐供应商</th>
                          <th className="num">预估单价</th>
                          <th className="num">预估金额</th>
                        </>
                      )}
                    </tr>
                  </thead>
                  <tbody>
                    {data.items.map(it => {
                      const isChecked = !!selected[it.item_id]
                      const estTotal = it.suggested_qty * (it.cost || 0)
                      return (
                        <tr
                          key={it.item_id}
                          className={canPurchase ? 'clickable' : ''}
                          onClick={() => {
                            if (canPurchase) toggleItem(it)
                          }}
                        >
                          {canPurchase && (
                            <td onClick={e => e.stopPropagation()}>
                              <input
                                type="checkbox"
                                className="check"
                                checked={isChecked}
                                onChange={() => toggleItem(it)}
                                aria-label={`勾选物料 ${it.code}`}
                              />
                            </td>
                          )}
                          <td>
                            <Link
                              className="mono link"
                              to={`/inventory/items/${it.item_id}`}
                              onClick={e => e.stopPropagation()}
                            >
                              {it.code}
                            </Link>
                            <span className="t2 ml-2">{it.name}</span>
                            <span className="cell-sub">{it.unit}</span>
                          </td>
                          <td className="num mono">
                            {it.available < 0 ? (
                              <span className="stock-short">
                                {it.available}
                                <span className="stock-tag-short">缺口 {Math.abs(it.available)}</span>
                              </span>
                            ) : (
                              it.available
                            )}
                          </td>
                          <td className="num mono t2">{it.safety_stock}</td>
                          <td className="num mono t2">{it.incoming > 0 ? it.incoming : '—'}</td>
                          <td className="num mono">
                            {it.suggested_qty > 0 ? (
                              <span className="replenishment-suggested">{it.suggested_qty}</span>
                            ) : (
                              <span className="t3">0</span>
                            )}
                          </td>
                          {canSeeCost && (
                            <>
                              <td>
                                {it.supplier_name ? (
                                  <span>{it.supplier_name}</span>
                                ) : (
                                  <span className="t3">—</span>
                                )}
                              </td>
                              <td className="num mono t2">{it.cost == null ? '—' : money(it.cost, it.currency || 'CNY')}</td>
                              <td className="num mono">
                                {it.suggested_qty > 0 && it.cost ? money(estTotal) : '—'}
                              </td>
                            </>
                          )}
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            )}

            {data && <Pager total={data.total} page={page} setPage={n => setParam('page', String(n))} />}
          </section>

          {/* 底部勾选批量操作栏 */}
          {canPurchase && selectedCount > 0 && (
            <div className="replenish-batch-bar" role="region" aria-label="批量采购操作">
              <div className="batch-info">
                <span>已选择 <strong>{selectedCount}</strong> 项物料</span>
                {supplierGrouping.isUniform && (
                  <span className="t2">（供应商：{supplierGrouping.singleSupplierName}）</span>
                )}
                {supplierGrouping.supplierCount > 1 && (
                  <span className="stock-short fs-12 inline intake-gap-sm">
                    <AlertCircle size={14} /> 所选物料涉及 {supplierGrouping.supplierCount} 家供应商
                  </span>
                )}
              </div>
              <span className="grow" />
              <button
                className="btn btn-secondary btn-sm"
                onClick={() => setSelected({})}
              >
                清空选择
              </button>
              <button
                className="btn btn-primary btn-sm"
                onClick={() => setDraftDrawer(true)}
              >
                <Plus /> 转为采购草稿
              </button>
            </div>
          )}
        </main>
      </div>

      {draftDrawer && (
        <ReplenishToDraftDrawer
          items={selectedItems}
          initialSupplierID={supplierGrouping.singleSupplierID}
          onClose={() => setDraftDrawer(false)}
          onDone={poId => {
            setDraftDrawer(false)
            setSelected({})
            nav(`/purchase-orders/${poId}`)
          }}
        />
      )}
    </>
  )
}

type DraftLine = {
  item_id: number
  code: string
  name: string
  unit: string
  qty: string
  price: string
}

function ReplenishToDraftDrawer({
  items,
  initialSupplierID = 0,
  onClose,
  onDone,
}: {
  items: ReplenishmentItem[]
  initialSupplierID?: number
  onClose: () => void
  onDone: (poId: number) => void
}) {
  const toast = useToast()
  const { data: suppliers } = useRecord<Page<Supplier>>('/api/suppliers?archived=1&page_size=200')

  const [supplier, setSupplier] = useState(initialSupplierID || 0)
  const [draftMode, setDraftMode] = useState<'new' | 'existing'>('new')
  const [targetDraftID, setTargetDraftID] = useState<number>(0)
  const [currency, setCurrency] = useState(items[0]?.currency || 'CNY')
  const [expected, setExpected] = useState('')
  const [shipping, setShipping] = useState('0')
  const [note, setNote] = useState('由库存补货建议批量生成')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  // 查该供应商已有的草稿采购单
  const { data: existingDrafts } = useRecord<Page<Purchase>>(
    supplier > 0 ? `/api/purchase-orders?status=draft&supplier_id=${supplier}&page_size=50` : ''
  )

  const [lines, setLines] = useState<DraftLine[]>(() =>
    items.map(it => ({
      item_id: it.item_id,
      code: it.code,
      name: it.name,
      unit: it.unit,
      qty: String(Math.max(1, it.suggested_qty)),
      price: fenToInput(it.cost ?? 0),
    }))
  )

  const updateLine = (idx: number, patch: Partial<DraftLine>) => {
    setLines(ls => ls.map((l, i) => (i === idx ? { ...l, ...patch } : l)))
  }

  const removeLine = (idx: number) => {
    setLines(ls => ls.filter((_, i) => i !== idx))
  }

  const totalGoods = lines.reduce((sum, l) => sum + Math.round(Number(l.qty || 0) * toFen(l.price || '0')), 0)
  const totalAmount = totalGoods + toFen(shipping)

  const isValid =
    supplier > 0 &&
    lines.length > 0 &&
    lines.every(
      l =>
        l.item_id > 0 &&
        Number.isSafeInteger(Number(l.qty)) &&
        Number(l.qty) > 0 &&
        Number(l.price) >= 0 && Number.isSafeInteger(Math.round(Number(l.qty) * toFen(l.price)))
    ) &&
    (draftMode === 'new' || targetDraftID > 0)

  const handleConfirm = async () => {
    if (!isValid || busy) return
    setBusy(true)
    setError('')

    try {
      if (draftMode === 'new') {
        const payload = {
          supplier_id: supplier,
          currency,
          shipping_fee: toFen(shipping),
          expected_at: expected,
          note,
          lines: lines.map(l => ({
            item_id: l.item_id,
            qty: Number(l.qty),
            amount: Math.round(Number(l.qty) * toFen(l.price)),
          })),
        }
        const res = await api<{ id: number }>('/api/purchase-orders', {
          method: 'POST',
          body: payload,
        })
        toast('采购草稿已创建', 'ok')
        onDone(res.id)
      } else {
        // 追加至已有草稿采购单
        // List rows do not include lines: load the current full draft before
        // merging, otherwise PATCH would replace and lose its existing lines.
        const targetOrder = await api<Purchase>(`/api/purchase-orders/${targetDraftID}`)
        if (!targetOrder) throw new Error('未找到目标草稿采购单')
        if (targetOrder.status !== 'draft' || targetOrder.supplier_id !== supplier) throw new Error('目标采购单状态或供应商已变化，请重新选择')
        if (targetOrder.currency !== currency) throw new Error('目标草稿币种不同，请统一币种后再追加')

        const currentLines = (targetOrder.lines || []).map(l => ({
          item_id: l.item_id,
          qty: l.qty,
          amount: l.amount,
        }))

        for (const l of lines) {
          const addedAmount = Math.round(Number(l.qty) * toFen(l.price))
          const exist = currentLines.find(x => x.item_id === l.item_id)
          if (exist) {
            exist.qty += Number(l.qty)
            exist.amount += addedAmount
          } else {
            currentLines.push({
              item_id: l.item_id,
              qty: Number(l.qty),
              amount: addedAmount,
            })
          }
        }

        await api(`/api/purchase-orders/${targetDraftID}`, {
          method: 'PATCH',
          body: {
            lines: currentLines,
          },
        })
        toast('已成功追加至采购草稿', 'ok')
        onDone(targetDraftID)
      }
    } catch (e) {
      setError(errText(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Drawer
      title="转为采购草稿"
      width={780}
      onClose={() => {
        if (!busy) onClose()
      }}
      foot={
        <>
          <span className="t2 mono">预估合计 {money(totalAmount, currency)}</span>
          <span className="grow" />
          <button className="btn btn-secondary" disabled={busy} onClick={onClose}>
            取消
          </button>
          <button
            className="btn btn-primary"
            disabled={busy || !isValid}
            onClick={handleConfirm}
          >
            {busy ? '正在生成草稿…' : '确认生成草稿'}
          </button>
        </>
      }
    >
      <div className="form-row">
        <Field label="供应商" req hint="同批草稿需为同一供应商">
          <select
            className="input"
            value={supplier}
            onChange={e => {
              setSupplier(Number(e.target.value))
              setTargetDraftID(0)
            }}
          >
            <option value={0}>选择供应商</option>
            {suppliers?.items.map(s => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </select>
        </Field>

        <Field label="草稿归属">
          <div className="inline intake-gap intake-mt-sm">
            <label className="inline">
              <input
                type="radio"
                name="draftMode"
                checked={draftMode === 'new'}
                onChange={() => setDraftMode('new')}
              />
              新建草稿
            </label>
            <label className="inline">
              <input
                type="radio"
                name="draftMode"
                disabled={!existingDrafts?.items.length}
                checked={draftMode === 'existing'}
                onChange={() => setDraftMode('existing')}
              />
              追加至已有草稿 ({existingDrafts?.items.length || 0})
            </label>
          </div>
        </Field>
      </div>

      {draftMode === 'existing' && (
        <Field label="选择已有草稿单" req>
          <select
            className="input mono"
            value={targetDraftID}
            onChange={e => {
              const id = Number(e.target.value)
              setTargetDraftID(id)
              const draft = existingDrafts?.items.find(p => p.id === id)
              if (draft) setCurrency(draft.currency)
            }}
          >
            <option value={0}>请选择要追加的草稿单</option>
            {existingDrafts?.items.map(p => (
              <option key={p.id} value={p.id}>
                {p.no} · 金额 {money(p.goods_amount, p.currency)}
              </option>
            ))}
          </select>
        </Field>
      )}

      {draftMode === 'new' && (
        <>
          <div className="form-row">
            <Field label="币种">
              <select
                className="input mono"
                value={currency}
                onChange={e => setCurrency(e.target.value)}
              >
                <option value="CNY">CNY</option>
                <option value="USD">USD</option>
                <option value="TWD">TWD</option>
              </select>
            </Field>
            <Field label="预计到货">
              <input
                className="input mono"
                type="date"
                value={expected}
                onChange={e => setExpected(e.target.value)}
              />
            </Field>
          </div>
          <Field label="运费（元）">
            <input
              className="input mono"
              type="number"
              min="0"
              step="0.01"
              value={shipping}
              onChange={e => setShipping(e.target.value)}
            />
          </Field>
        </>
      )}

      <div className="panel mt-3">
        <div className="panel-head">
          <div className="panel-title">采购明细（{lines.length}）</div>
        </div>
        <div className="table-wrap">
          <table className="table recognize-lines-table">
            <thead>
              <tr>
                <th>物料编码与名称</th>
                <th style={{ width: 110 }}>补货数量</th>
                <th style={{ width: 110 }}>单价（元）</th>
                <th className="num" style={{ width: 110 }}>小计</th>
                <th style={{ width: 44 }}></th>
              </tr>
            </thead>
            <tbody>
              {lines.map((l, i) => {
                const sub = Math.round(Number(l.qty || 0) * toFen(l.price || '0'))
                return (
                  <tr key={l.item_id}>
                    <td>
                      <div className="mono intake-medium">{l.code}</div>
                      <div className="cell-sub">{l.name} ({l.unit})</div>
                    </td>
                    <td>
                      <input
                        className="input mono"
                        type="number"
                        min="1"
                        step="1"
                        value={l.qty}
                        onChange={e => updateLine(i, { qty: e.target.value })}
                        aria-label="数量"
                      />
                    </td>
                    <td>
                      <input
                        className="input mono"
                        type="number"
                        min="0"
                        step="0.01"
                        value={l.price}
                        onChange={e => updateLine(i, { price: e.target.value })}
                        aria-label="单价"
                      />
                    </td>
                    <td className="num mono intake-medium">{money(sub, currency)}</td>
                    <td>
                      <button
                        className="btn btn-ghost btn-icon btn-sm"
                        title="移除此行"
                        disabled={lines.length === 1}
                        onClick={() => removeLine(i)}
                      >
                        ×
                      </button>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      </div>

      {draftMode === 'new' && (
        <Field label="采购备注">
          <textarea
            className="textarea"
            rows={2}
            maxLength={1000}
            value={note}
            onChange={e => setNote(e.target.value)}
          />
        </Field>
      )}

      {error && <div className="form-err" role="alert">{error}</div>}
    </Drawer>
  )
}
