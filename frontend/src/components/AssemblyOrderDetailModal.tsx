import { useState } from 'react'
import { Link } from 'react-router-dom'
import { CheckCircle2, Ban, Layers } from 'lucide-react'
import { api } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { fmtDate } from '../lib/format'
import { Drawer, Pill } from './ui'
import {
  assemblyStatus,
  formatBps,
  formatMoney,
  type AssemblyOrder,
} from '../lib/assembly'

export function AssemblyOrderDetailModal({
  order,
  onClose,
  onReload,
}: {
  order: AssemblyOrder
  onClose: () => void
  onReload: () => void
}) {
  const { can, isAdmin } = useAuth()
  const toast = useToast()

  const canEdit = isAdmin || can('inventory.edit')
  const canViewCost = isAdmin || can('product.cost') || can('finance.view')

  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [confirmAction, setConfirmAction] = useState<'' | 'complete' | 'cancel'>('')

  const handleComplete = async () => {
    setError('')
    setBusy(true)
    try {
      await api(`/api/assembly/orders/${order.id}/complete`, { method: 'POST' })
      toast('组装加工单已完成，原料已出库，成品已入库', 'ok')
      setConfirmAction('')
      onReload()
    } catch (err) {
      setError(errText(err))
    } finally {
      setBusy(false)
    }
  }

  const handleCancel = async () => {
    setError('')
    setBusy(true)
    try {
      await api(`/api/assembly/orders/${order.id}/cancel`, { method: 'POST' })
      toast('组装加工单已取消', 'ok')
      setConfirmAction('')
      onReload()
    } catch (err) {
      setError(errText(err))
    } finally {
      setBusy(false)
    }
  }

  const st = assemblyStatus[order.status] || { label: order.status, pill: 'pill-neutral' }
  const isDraft = order.status === 'draft'

  const unitCostFen =
    canViewCost && typeof order.total_cost === 'number' && order.output_qty > 0
      ? Math.round(order.total_cost / order.output_qty)
      : null

  return (
    <Drawer
      title={
        <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
          <Layers style={{ width: 18, height: 18, color: 'var(--primary)' }} />
          <span>组装单详情 · <span className="mono">{order.no}</span></span>
          <Pill kind={st.pill}>{st.label}</Pill>
        </div>
      }
      width={680}
      onClose={onClose}
      foot={
        <div style={{ display: 'flex', width: '100%', gap: 8, justifyContent: 'flex-end' }}>
          {isDraft && canEdit && (
            <>
              <button
                className="btn btn-danger"
                disabled={busy}
                onClick={() => setConfirmAction('cancel')}
                type="button"
              >
                <Ban />
                取消单据
              </button>
              <button
                className="btn btn-primary"
                disabled={busy}
                onClick={() => setConfirmAction('complete')}
                type="button"
              >
                <CheckCircle2 />
                完成组装 (执行扣料入库)
              </button>
            </>
          )}
          <button className="btn btn-secondary" onClick={onClose} type="button">
            关闭
          </button>
        </div>
      }
    >
      <div className="assembly-drawer-body">
        {error && <div className="assembly-alert">{error}</div>}

        {confirmAction === 'complete' && (
          <div className="assembly-alert" style={{ background: 'var(--success-bg)', borderColor: 'var(--success-bg)', color: 'var(--success-text)' }}>
            <div style={{ fontWeight: 600, marginBottom: 4 }}>确认完成组装加工？</div>
            <div>
              系统将通过事务自动从 <strong>{order.warehouse_name}</strong> 扣减所需原料（先进先出、不占用订单预留），
              并按原料实际批次成本加算工费生成成品入库批次。
            </div>
            <div style={{ display: 'flex', gap: 8, marginTop: 10 }}>
              <button
                className="btn btn-primary btn-sm"
                disabled={busy}
                onClick={handleComplete}
                type="button"
              >
                {busy ? '正在执行…' : '确认执行完成'}
              </button>
              <button
                className="btn btn-secondary btn-sm"
                disabled={busy}
                onClick={() => setConfirmAction('')}
                type="button"
              >
                取消
              </button>
            </div>
          </div>
        )}

        {confirmAction === 'cancel' && (
          <div className="assembly-alert">
            <div style={{ fontWeight: 600, marginBottom: 4 }}>确认取消该草稿单？</div>
            <div>取消后单据将保留为取消记录，无法再次完成。</div>
            <div style={{ display: 'flex', gap: 8, marginTop: 10 }}>
              <button
                className="btn btn-danger btn-sm"
                disabled={busy}
                onClick={handleCancel}
                type="button"
              >
                {busy ? '正在取消…' : '确认取消'}
              </button>
              <button
                className="btn btn-secondary btn-sm"
                disabled={busy}
                onClick={() => setConfirmAction('')}
                type="button"
              >
                返回
              </button>
            </div>
          </div>
        )}

        {/* 基础信息卡片 */}
        <div className="panel" style={{ padding: 14 }}>
          <div className="assembly-detail-layout">
            <dl className="assembly-kv">
              <dt>组装单号</dt>
              <dd className="mono">{order.no}</dd>
              <dt>单据状态</dt>
              <dd>
                <Pill kind={st.pill}>{st.label}</Pill>
              </dd>
              <dt>产出成品</dt>
              <dd>
                <Link className="mono link" to={`/inventory/items/${order.output_item_id}`}>
                  {order.output_item_code}
                </Link>{' '}
                {order.output_item_name}
              </dd>
              <dt>产出数量</dt>
              <dd className="mono" style={{ fontWeight: 600 }}>
                {order.output_qty} {order.output_item_unit || '件'} (共 {order.runs} 批)
              </dd>
            </dl>

            <dl className="assembly-kv">
              <dt>加工仓库</dt>
              <dd>{order.warehouse_name}</dd>
              <dt>创建时间</dt>
              <dd className="mono t3">{fmtDate(order.created_at, true)}</dd>
              {order.done_at && (
                <>
                  <dt>完成时间</dt>
                  <dd className="mono t3">{fmtDate(order.done_at, true)}</dd>
                </>
              )}
              {order.canceled_at && (
                <>
                  <dt>取消时间</dt>
                  <dd className="mono t3">{fmtDate(order.canceled_at, true)}</dd>
                </>
              )}
              {order.output_batch_id && (
                <>
                  <dt>产物批次</dt>
                  <dd className="mono">#{order.output_batch_id}</dd>
                </>
              )}
            </dl>
          </div>
        </div>

        {/* 成本核算卡片（仅拥有 product.cost 或 finance.view 权限显示） */}
        {canViewCost && (
          <div className="panel" style={{ padding: 14, background: 'var(--canvas)' }}>
            <div style={{ fontWeight: 600, fontSize: 13, marginBottom: 8 }}>成本与工费核算</div>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: 12 }}>
              <div>
                <div style={{ fontSize: 11, color: 'var(--text-3)' }}>加工工费</div>
                <div className="mono" style={{ fontSize: 14, fontWeight: 600, marginTop: 2 }}>
                  {formatMoney(order.labor_cost, order.currency)}
                </div>
              </div>
              <div>
                <div style={{ fontSize: 11, color: 'var(--text-3)' }}>原料实际成本</div>
                <div className="mono" style={{ fontSize: 14, fontWeight: 600, marginTop: 2 }}>
                  {order.status === 'done' ? formatMoney(order.material_cost, order.currency) : '待完成核算'}
                </div>
              </div>
              <div>
                <div style={{ fontSize: 11, color: 'var(--text-3)' }}>组装总成本</div>
                <div className="mono" style={{ fontSize: 14, fontWeight: 600, marginTop: 2, color: 'var(--primary-text)' }}>
                  {order.status === 'done' ? formatMoney(order.total_cost, order.currency) : '待完成核算'}
                </div>
              </div>
              <div>
                <div style={{ fontSize: 11, color: 'var(--text-3)' }}>成品单位成本</div>
                <div className="mono" style={{ fontSize: 14, fontWeight: 600, marginTop: 2 }}>
                  {unitCostFen !== null ? formatMoney(unitCostFen, order.currency) : '—'}
                </div>
              </div>
            </div>
          </div>
        )}

        {/* 原料扣减清单 */}
        <div className="panel" style={{ padding: 14 }}>
          <div style={{ fontWeight: 600, fontSize: 13, marginBottom: 8 }}>原料配件扣减清单</div>
          <div className="assembly-table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>物料</th>
                  <th className="num">单批用量</th>
                  <th className="num">损耗率</th>
                  <th className="num">需扣总量</th>
                  {canViewCost && order.status === 'done' && <th className="num">实际扣减成本</th>}
                </tr>
              </thead>
              <tbody>
                {order.lines?.map(l => (
                  <tr key={l.id}>
                    <td>
                      <Link className="mono link" to={`/inventory/items/${l.item_id}`}>
                        {l.item_code}
                      </Link>
                      <div className="cell-sub">{l.item_name}</div>
                    </td>
                    <td className="num mono">
                      {l.unit_qty} {l.item_unit || '件'}
                    </td>
                    <td className="num mono t2">{formatBps(l.loss_bps)}</td>
                    <td className="num mono" style={{ fontWeight: 600 }}>
                      {l.required_qty} {l.item_unit || '件'}
                    </td>
                    {canViewCost && order.status === 'done' && (
                      <td className="num mono">{formatMoney(l.actual_cost, l.currency)}</td>
                    )}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>

        {/* 配方快照与关联信息 */}
        {order.recipe_snapshot && (
          <div className="panel" style={{ padding: 14, fontSize: 12, color: 'var(--text-2)' }}>
            <div style={{ fontWeight: 600, fontSize: 13, marginBottom: 6, color: 'var(--text)' }}>
              配方历史快照
            </div>
            <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap' }}>
              <span>
                基准配方：<strong>{order.recipe_snapshot.name}</strong>
              </span>
              <span>
                基准产量：{order.recipe_snapshot.output_qty} {order.recipe_snapshot.output_item_unit}
              </span>
              {order.recipe_snapshot.article_id && (
                <span>
                  关联知识库：
                  <Link
                    className="link mono"
                    to={`/kb/${order.recipe_snapshot.article_id}`}
                    target="_blank"
                    rel="noreferrer"
                  >
                    工艺文章 #{order.recipe_snapshot.article_id}
                  </Link>
                </span>
              )}
            </div>
            {order.recipe_snapshot.note && (
              <div style={{ marginTop: 6, color: 'var(--text-3)' }}>
                配方说明：{order.recipe_snapshot.note}
              </div>
            )}
          </div>
        )}

        {/* 备注 */}
        {order.note && (
          <div className="panel" style={{ padding: 14 }}>
            <div style={{ fontSize: 12, color: 'var(--text-3)', marginBottom: 4 }}>单据备注</div>
            <div style={{ fontSize: 13 }}>{order.note}</div>
          </div>
        )}
      </div>
    </Drawer>
  )
}
