import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { Plus, Search, Play, Pencil, BookOpen } from 'lucide-react'
import { api, qs, type Page } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { fmtDate } from '../lib/format'
import { Topbar, Empty, Pill, Tabs, Loading } from '../components/ui'
import { Pager, RecordState, useRecord } from '../components/CommerceShared'
import { useWarehouses } from '../components/InventoryForms'
import {
  assemblyStatus,
  formatMoney,
  type AssemblyOrder,
  type AssemblyRecipe,
} from '../lib/assembly'
import { AssemblyRecipeDrawer } from '../components/AssemblyRecipeDrawer'
import { AssemblyOrderDrawer } from '../components/AssemblyOrderDrawer'
import { AssemblyOrderDetailModal } from '../components/AssemblyOrderDetailModal'
import '../styles/assembly.css'

export function Assembly() {
  const { can, isAdmin } = useAuth()
  const toast = useToast()
  const [sp, setSp] = useSearchParams()

  const tab = (sp.get('tab') as 'orders' | 'recipes') || 'orders'
  const status = sp.get('status') || ''
  const warehouse = sp.get('warehouse_id') || ''
  const archived = sp.get('archived') || '0'
  const q = sp.get('q') || ''
  const page = Number(sp.get('page')) || 1

  const setParam = (key: string, value: string) => {
    const next = new URLSearchParams(sp)
    if (value) next.set(key, value)
    else next.delete(key)
    if (key !== 'page') next.delete('page')
    setSp(next, { replace: true })
  }

  const { items: warehouses } = useWarehouses(false)
  const canEdit = isAdmin || can('inventory.edit')
  const canViewCost = isAdmin || can('product.cost') || can('finance.view')

  // Orders query
  const ordersQuery = qs({
    status,
    warehouse_id: warehouse,
    q: tab === 'orders' ? q : '',
    page: tab === 'orders' ? page : 1,
    page_size: 50,
  })
  const {
    data: ordersData,
    error: ordersError,
    reload: reloadOrders,
  } = useRecord<Page<AssemblyOrder>>('/api/assembly/orders' + ordersQuery)

  // Recipes query
  const recipesQuery = qs({
    archived,
    q: tab === 'recipes' ? q : '',
    page: tab === 'recipes' ? page : 1,
    page_size: 50,
  })
  const {
    data: recipesData,
    error: recipesError,
    reload: reloadRecipes,
  } = useRecord<Page<AssemblyRecipe>>('/api/assembly/recipes' + recipesQuery)

  // Drawers state
  const [recipeDrawer, setRecipeDrawer] = useState(false)
  const [editingRecipe, setEditingRecipe] = useState<AssemblyRecipe | null>(null)
  const [orderDrawer, setOrderDrawer] = useState(false)
  const [orderDefaultRecipeId, setOrderDefaultRecipeId] = useState<number | undefined>(undefined)
  const [selectedOrder, setSelectedOrder] = useState<AssemblyOrder | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)

  const openOrder = (id: number) => {
    setDetailLoading(true)
    api<{ order: AssemblyOrder }>(`/api/assembly/orders/${id}`).then(r => setSelectedOrder(r.order)).catch(e => toast(errText(e), 'error')).finally(() => setDetailLoading(false))
  }
  const openRecipe = (id: number) => {
    api<{ recipe: AssemblyRecipe }>(`/api/assembly/recipes/${id}`).then(r => { setEditingRecipe(r.recipe); setRecipeDrawer(true) }).catch(e => toast(errText(e), 'error'))
  }
  const linkedOrder = Number(sp.get('order_id')) || 0
  useEffect(() => { if (linkedOrder > 0 && can('inventory.view')) openOrder(linkedOrder) }, [linkedOrder])

  if (!can('inventory.view')) {
    return (
      <>
        <Topbar title="组装加工" />
        <Empty title="没有查看库存或组装加工的权限" />
      </>
    )
  }

  const handleOpenCreateOrder = (recipeId?: number) => {
    setOrderDefaultRecipeId(recipeId)
    setOrderDrawer(true)
  }

  return (
    <>
      <Topbar title="组装加工" sub="配方、原料用量与成品入库">
        {canEdit && (
          <>
            <button
              className="btn btn-secondary"
              onClick={() => {
                setEditingRecipe(null)
                setRecipeDrawer(true)
              }}
            >
              <Plus />
              新建配方
            </button>
            <button className="btn btn-primary" onClick={() => handleOpenCreateOrder()}>
              <Play />
              新建组装单
            </button>
          </>
        )}
      </Topbar>

      <div className="scroll">
        <main className="content">
          {detailLoading && <Loading />}
          <Tabs
            value={tab}
            onChange={v => setParam('tab', v)}
            items={[
              { key: 'orders', label: '组装加工单' },
              { key: 'recipes', label: '配方库' },
            ]}
          />

          {tab === 'orders' ? (
            <>
              <div className="toolbar">
                <select
                  className="input assembly-select"
                  aria-label="单据状态筛选"
                  value={status}
                  onChange={e => setParam('status', e.target.value)}
                >
                  <option value="">全部状态</option>
                  <option value="draft">草稿</option>
                  <option value="done">已完成</option>
                  <option value="canceled">已取消</option>
                </select>

                <select
                  className="input assembly-select"
                  aria-label="加工仓库筛选"
                  value={warehouse}
                  onChange={e => setParam('warehouse_id', e.target.value)}
                >
                  <option value="">全部仓库</option>
                  {(warehouses || []).map(w => (
                    <option key={w.id} value={w.id}>
                      {w.name}
                    </option>
                  ))}
                </select>

                <span className="spacer" />

                <label className="field assembly-search">
                  <Search />
                  <input
                    placeholder="搜索单号、配方或物料"
                    value={q}
                    onChange={e => setParam('q', e.target.value)}
                  />
                </label>
              </div>

              <section className="panel">
                {!ordersData ? (
                  <RecordState error={ordersError} reload={reloadOrders} />
                ) : !ordersData.items.length ? (
                  <Empty
                    title={status || q || warehouse ? '没有符合条件的组装单' : '还没有组装单'}
                    desc="基于已有配方创建组装单，草稿一键完成扣原料、入库成品。"
                  >
                    {canEdit && (
                      <button className="btn btn-primary" onClick={() => handleOpenCreateOrder()}>
                        <Play />
                        创建首张组装单
                      </button>
                    )}
                  </Empty>
                ) : (
                  <div className="assembly-table-wrap">
                    <table className="table">
                      <thead>
                        <tr>
                          <th>组装单号</th>
                          <th>产出成品</th>
                          <th className="num">产出数量</th>
                          <th className="num">批数</th>
                          <th>加工仓库</th>
                          <th>状态</th>
                          {canViewCost && <th className="num">加工工费</th>}
                          {canViewCost && <th className="num">原料成本</th>}
                          {canViewCost && <th className="num">总成本</th>}
                          <th>创建时间</th>
                        </tr>
                      </thead>
                      <tbody>
                        {ordersData.items.map(o => {
                          const st = assemblyStatus[o.status] || {
                            label: o.status,
                            pill: 'pill-neutral',
                          }
                          return (
                            <tr
                              key={o.id}
                              className="assembly-clickable"
                              onClick={() => openOrder(o.id)}
                            >
                              <td>
                                <span className="mono link">{o.no}</span>
                                {o.recipe_name && <div className="cell-sub">{o.recipe_name}</div>}
                              </td>
                              <td>
                                <Link
                                  className="mono link"
                                  to={`/inventory/items/${o.output_item_id}`}
                                  onClick={e => e.stopPropagation()}
                                >
                                  {o.output_item_code}
                                </Link>
                                <div className="cell-sub">{o.output_item_name}</div>
                              </td>
                              <td className="num mono" style={{ fontWeight: 600 }}>
                                {o.output_qty} {o.output_item_unit || '件'}
                              </td>
                              <td className="num mono t2">{o.runs}</td>
                              <td>{o.warehouse_name}</td>
                              <td>
                                <Pill kind={st.pill}>{st.label}</Pill>
                              </td>
                              {canViewCost && (
                                <td className="num mono t2">{formatMoney(o.labor_cost, o.currency)}</td>
                              )}
                              {canViewCost && (
                                <td className="num mono t2">
                                  {o.status === 'done' ? formatMoney(o.material_cost, o.currency) : '—'}
                                </td>
                              )}
                              {canViewCost && (
                                <td className="num mono">
                                  {o.status === 'done' ? formatMoney(o.total_cost, o.currency) : '—'}
                                </td>
                              )}
                              <td className="mono t3">{fmtDate(o.created_at)}</td>
                            </tr>
                          )
                        })}
                      </tbody>
                    </table>
                  </div>
                )}
                {ordersData && (
                  <Pager
                    total={ordersData.total}
                    page={page}
                    setPage={n => setParam('page', String(n))}
                  />
                )}
              </section>
            </>
          ) : (
            <>
              <div className="toolbar">
                <select
                  className="input assembly-select"
                  aria-label="配方归档筛选"
                  value={archived}
                  onChange={e => setParam('archived', e.target.value)}
                >
                  <option value="0">正常配方</option>
                  <option value="all">全部配方</option>
                  <option value="1">已归档配方</option>
                </select>

                <span className="spacer" />

                <label className="field assembly-search">
                  <Search />
                  <input
                    placeholder="搜索配方或物料"
                    value={q}
                    onChange={e => setParam('q', e.target.value)}
                  />
                </label>
              </div>

              <section className="panel">
                {!recipesData ? (
                  <RecordState error={recipesError} reload={reloadRecipes} />
                ) : !recipesData.items.length ? (
                  <Empty
                    title={q || archived !== '0' ? '没有符合条件的配方' : '还没有配方'}
                    desc="配方定义了产出物料、单次产量、原料配件用量及加工损耗率。"
                  >
                    {canEdit && (
                      <button
                        className="btn btn-primary"
                        onClick={() => {
                          setEditingRecipe(null)
                          setRecipeDrawer(true)
                        }}
                      >
                        <Plus />
                        新建首个配方
                      </button>
                    )}
                  </Empty>
                ) : (
                  <div className="assembly-table-wrap">
                    <table className="table">
                      <thead>
                        <tr>
                          <th>配方名称</th>
                          <th>产出物料</th>
                          <th className="num">单次产量</th>
                          <th className="num">原料配件</th>
                          {canViewCost && <th className="num">工费</th>}
                          <th>工艺标准</th>
                          <th>状态</th>
                          <th>更新时间</th>
                          {canEdit && <th className="num">操作</th>}
                        </tr>
                      </thead>
                      <tbody>
                        {recipesData.items.map(r => (
                          <tr key={r.id}>
                            <td>
                              <div style={{ fontWeight: 600 }}>{r.name}</div>
                              {r.note && <div className="cell-sub">{r.note}</div>}
                            </td>
                            <td>
                              <Link
                                className="mono link"
                                to={`/inventory/items/${r.output_item_id}`}
                              >
                                {r.output_item_code}
                              </Link>
                              <div className="cell-sub">{r.output_item_name}</div>
                            </td>
                            <td className="num mono">
                              {r.output_qty} {r.output_item_unit || '件'}
                            </td>
                            <td className="num mono">
                              <span className="chip outline">
                                {r.lines_count ?? r.lines?.length ?? 0} 种物料
                              </span>
                            </td>
                            {canViewCost && (
                              <td className="num mono">{formatMoney(r.labor_cost, r.currency)}</td>
                            )}
                            <td>
                              {r.article_id ? (
                                <Link
                                  className="mono link"
                                  to={`/kb/${r.article_id}`}
                                  target="_blank"
                                  rel="noreferrer"
                                  title={r.article_title || '工艺文章'}
                                >
                                  <BookOpen style={{ width: 14, height: 14, verticalAlign: -2 }} />{' '}
                                  {r.article_title || `文章 #${r.article_id}`}
                                </Link>
                              ) : (
                                <span className="t3">—</span>
                              )}
                            </td>
                            <td>
                              <Pill kind={r.archived ? 'pill-neutral' : 'pill-success'}>
                                {r.archived ? '已归档' : '正常'}
                              </Pill>
                            </td>
                            <td className="mono t3">{fmtDate(r.updated_at)}</td>
                            {canEdit && (
                              <td className="num">
                                <div style={{ display: 'inline-flex', gap: 6 }}>
                                  {!r.archived && (
                                    <button
                                      className="btn btn-outline btn-sm"
                                      onClick={() => handleOpenCreateOrder(r.id)}
                                      title="基于此配方创建组装单"
                                    >
                                      <Play />
                                      建单
                                    </button>
                                  )}
                                  <button
                                    className="btn btn-ghost btn-sm"
                                    onClick={() => {
                                      openRecipe(r.id)
                                    }}
                                    title="编辑配方"
                                  >
                                    <Pencil />
                                    编辑
                                  </button>
                                </div>
                              </td>
                            )}
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}
                {recipesData && (
                  <Pager
                    total={recipesData.total}
                    page={page}
                    setPage={n => setParam('page', String(n))}
                  />
                )}
              </section>
            </>
          )}
        </main>
      </div>

      {/* 配方编辑抽屉 */}
      {recipeDrawer && (
        <AssemblyRecipeDrawer
          value={editingRecipe}
          onClose={() => setRecipeDrawer(false)}
          onDone={() => {
            setRecipeDrawer(false)
            reloadRecipes()
          }}
        />
      )}

      {/* 组装单创建抽屉 */}
      {orderDrawer && (
        <AssemblyOrderDrawer
          recipes={recipesData?.items || []}
          defaultRecipeId={orderDefaultRecipeId}
          onClose={() => setOrderDrawer(false)}
          onDone={newOrder => {
            setOrderDrawer(false)
            reloadOrders()
            setSelectedOrder(newOrder)
          }}
        />
      )}

      {/* 组装单详情抽屉 */}
      {selectedOrder && (
        <AssemblyOrderDetailModal
          order={selectedOrder}
          onClose={() => setSelectedOrder(null)}
          onReload={() => {
            reloadOrders()
            setSelectedOrder(null)
          }}
        />
      )}
    </>
  )
}
