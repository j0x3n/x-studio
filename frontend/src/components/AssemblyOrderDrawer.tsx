import { useEffect, useState, useMemo } from 'react'
import { api } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { Drawer, Field } from './ui'
import { useWarehouses } from './InventoryForms'
import {
  calcRequiredQty,
  formatBps,
  formatMoney,
  type AssemblyOrder,
  type AssemblyRecipe,
} from '../lib/assembly'

export function AssemblyOrderDrawer({
  recipes,
  defaultRecipeId,
  onClose,
  onDone,
}: {
  recipes: AssemblyRecipe[]
  defaultRecipeId?: number
  onClose: () => void
  onDone: (order: AssemblyOrder) => void
}) {
  const { can, isAdmin } = useAuth()
  const toast = useToast()
  const canViewCost = isAdmin || can('product.cost') || can('finance.view')

  const { items: warehouses } = useWarehouses(false)
  // 组装加工仅支持自有仓或档口仓
  const allowedWarehouses = useMemo(
    () => (warehouses || []).filter(w => !w.archived && (w.type === 'own' || w.type === 'stall')),
    [warehouses]
  )

  const activeRecipes = useMemo(
    () => recipes.filter(r => !r.archived),
    [recipes]
  )

  const [recipeId, setRecipeId] = useState<number>(
    defaultRecipeId || activeRecipes[0]?.id || 0
  )
  const [warehouseId, setWarehouseId] = useState<number>(
    allowedWarehouses[0]?.id || 0
  )
  const [runs, setRuns] = useState<number>(1)
  const [note, setNote] = useState<string>('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [recipeDetail, setRecipeDetail] = useState<AssemblyRecipe | null>(null)
  useEffect(() => {
    if (!recipeId) { setRecipeDetail(null); return }
    const controller = new AbortController()
    setRecipeDetail(null)
    api<{ recipe: AssemblyRecipe }>(`/api/assembly/recipes/${recipeId}`, { signal: controller.signal }).then(r => setRecipeDetail(r.recipe)).catch(e => { if (!controller.signal.aborted) setError(errText(e)) })
    return () => controller.abort()
  }, [recipeId])

  const selectedRecipe = useMemo(
    () => recipeDetail || recipes.find(r => r.id === recipeId) || null,
    [recipeDetail, recipes, recipeId]
  )

  useEffect(() => { if (!warehouseId && allowedWarehouses[0]) setWarehouseId(allowedWarehouses[0].id) }, [allowedWarehouses, warehouseId])

  const totalOutputQty = (selectedRecipe?.output_qty || 0) * runs
  const totalLaborCost = typeof selectedRecipe?.labor_cost === 'number' ? selectedRecipe.labor_cost * runs : undefined

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')

    if (!recipeId) {
      setError('请选择加工配方')
      return
    }
    if (!warehouseId) {
      setError('请选择加工仓库（仅自有仓或档口仓）')
      return
    }
    if (!Number.isSafeInteger(runs) || runs <= 0) {
      setError('组装批数必须大于 0')
      return
    }

    setBusy(true)
    try {
      const res = await api<{ order: AssemblyOrder }>('/api/assembly/orders', {
        method: 'POST',
        body: {
          recipe_id: recipeId,
          runs,
          warehouse_id: warehouseId,
          note: note.trim(),
        },
      })
      toast('组装单草稿已创建', 'ok')
      onDone(res.order)
    } catch (err) {
      setError(errText(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Drawer
      title="创建组装加工单 (草稿)"
      width={500}
      onClose={() => {
        if (!busy) onClose()
      }}
      foot={
        <>
          <button className="btn btn-secondary" disabled={busy} onClick={onClose} type="button">
            取消
          </button>
          <button className="btn btn-primary grow" disabled={busy} onClick={handleSubmit} type="button">
            {busy ? '正在创建…' : '创建草稿单'}
          </button>
        </>
      }
    >
      <form className="assembly-drawer-body" onSubmit={handleSubmit}>
        {error && <div className="assembly-alert">{error}</div>}

        <Field label="选择配方" req>
          <select
            className="input"
            value={recipeId}
            onChange={e => setRecipeId(Number(e.target.value))}
            autoFocus
          >
            <option value={0}>请选择配方…</option>
            {activeRecipes.map(r => (
              <option key={r.id} value={r.id}>
                {r.name} (产出: {r.output_item_name || r.output_item_code} × {r.output_qty})
              </option>
            ))}
          </select>
        </Field>

        <div className="assembly-row-group">
          <Field label="组装加工仓库 (自有/档口)" req hint="原料从该仓扣减，产物在该仓入库">
            <select
              className="input"
              value={warehouseId}
              onChange={e => setWarehouseId(Number(e.target.value))}
            >
              {allowedWarehouses.length === 0 && <option value={0}>暂无可用自有仓或档口仓</option>}
              {allowedWarehouses.map(w => (
                <option key={w.id} value={w.id}>
                  {w.name} ({w.type === 'own' ? '自有仓' : '档口'})
                </option>
              ))}
            </select>
          </Field>

          <Field label="组装批数 (Runs)" req hint="执行几次配方">
            <input
              className="input mono"
              type="number"
              min={1}
              value={runs}
              onChange={e => setRuns(Math.max(1, parseInt(e.target.value, 10) || 1))}
            />
          </Field>
        </div>

        {selectedRecipe && (
          <div className="assembly-preview-card">
            <div className="assembly-preview-row">
              <span>预计成品产出：</span>
              <span className="assembly-preview-strong mono">
                {selectedRecipe.output_item_name} × {totalOutputQty} {selectedRecipe.output_item_unit || '件'}
              </span>
            </div>

            {canViewCost && typeof totalLaborCost === 'number' && (
              <div className="assembly-preview-row">
                <span>预计加工工费：</span>
                <span className="assembly-preview-strong mono">
                  {formatMoney(totalLaborCost, selectedRecipe.currency)}
                </span>
              </div>
            )}

            {selectedRecipe.lines && selectedRecipe.lines.length > 0 && (
              <div style={{ marginTop: 6 }}>
                <div style={{ fontWeight: 600, marginBottom: 4 }}>预计消耗原料清单（含损耗向上取整）：</div>
                {selectedRecipe.lines.map((l, i) => {
                  const req = calcRequiredQty(l.qty, runs, l.loss_bps)
                  return (
                    <div key={i} className="assembly-preview-row" style={{ color: 'var(--text-3)' }}>
                      <span>
                        {l.item_code} {l.item_name} (单批净用 {l.qty}，损耗 {formatBps(l.loss_bps)})
                      </span>
                      <span className="mono">
                        需扣 {req} {l.item_unit || '件'}
                      </span>
                    </div>
                  )
                })}
              </div>
            )}
          </div>
        )}

        <Field label="组装单备注">
          <textarea
            className="textarea"
            rows={3}
            placeholder="例如：第一批手工包金边定制组装"
            value={note}
            onChange={e => setNote(e.target.value)}
          />
        </Field>
      </form>
    </Drawer>
  )
}
