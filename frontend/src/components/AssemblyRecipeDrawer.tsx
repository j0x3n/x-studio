import { useEffect, useState } from 'react'
import { Plus, Trash2 } from 'lucide-react'
import { api, qs, type Page } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { Drawer, Field } from './ui'
import { ItemPicker } from './InventoryForms'
import type { InvItem } from '../lib/inventory'
import type { KbArticle } from '../lib/kb'
import {
  calcRequiredQty,
  type AssemblyRecipe,
  type RecipeLine,
} from '../lib/assembly'

type EditableLine = {
  item: InvItem | null
  qty: number
  lossPct: string // string for clean user typing of e.g. "5" for 5%
}

export function AssemblyRecipeDrawer({
  value,
  onClose,
  onDone,
}: {
  value?: AssemblyRecipe | null
  onClose: () => void
  onDone: (rec: AssemblyRecipe) => void
}) {
  const { can, isAdmin } = useAuth()
  const toast = useToast()
  const isEdit = Boolean(value)

  const canEditCost = isAdmin || can('product.cost') || can('finance.edit')
  const canViewKb = isAdmin || can('kb.view')

  const [name, setName] = useState(value?.name || '')
  const [outputItem, setOutputItem] = useState<InvItem | null>(
    value?.output_item_id
      ? ({
          id: value.output_item_id,
          code: value.output_item_code || '',
          name: value.output_item_name || '',
          unit: value.output_item_unit || '件',
        } as InvItem)
      : null
  )
  const [outputQty, setOutputQty] = useState(value?.output_qty || 1)
  const [currency, setCurrency] = useState(value?.currency || 'CNY')
  const [laborYuan, setLaborYuan] = useState(
    typeof value?.labor_cost === 'number' ? (value.labor_cost / 100).toFixed(2) : '0'
  )
  const [articleIdStr, setArticleIdStr] = useState(
    value?.article_id ? String(value.article_id) : ''
  )
  const [articleQuery, setArticleQuery] = useState('')
  const [articles, setArticles] = useState<KbArticle[]>([])
  useEffect(() => {
    if (!canViewKb) return
    const timer = setTimeout(() => {
      api<Page<KbArticle>>('/api/kb/articles' + qs({ q: articleQuery, page_size: 50 })).then(r => setArticles(r.items || [])).catch(() => setArticles([]))
    }, 200)
    return () => clearTimeout(timer)
  }, [canViewKb, articleQuery])
  const [note, setNote] = useState(value?.note || '')
  const [archived, setArchived] = useState(Boolean(value?.archived))

  const [lines, setLines] = useState<EditableLine[]>(() => {
    if (value?.lines && value.lines.length > 0) {
      return value.lines.map((l: RecipeLine) => ({
        item: {
          id: l.item_id,
          code: l.item_code || '',
          name: l.item_name || '',
          unit: l.item_unit || '件',
        } as InvItem,
        qty: l.qty,
        lossPct: String((l.loss_bps || 0) / 100),
      }))
    }
    return [{ item: null, qty: 1, lossPct: '0' }]
  })

  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const addLine = () => {
    setLines(prev => [...prev, { item: null, qty: 1, lossPct: '0' }])
  }

  const removeLine = (idx: number) => {
    setLines(prev => prev.filter((_, i) => i !== idx))
  }

  const updateLine = (idx: number, patch: Partial<EditableLine>) => {
    setLines(prev => prev.map((l, i) => (i === idx ? { ...l, ...patch } : l)))
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')

    const trimmedName = name.trim()
    if (!trimmedName) {
      setError('请输入配方名称')
      return
    }
    if (!outputItem) {
      setError('请选择产出物料（成品 SKU 或组合件）')
      return
    }
    if (!Number.isSafeInteger(outputQty) || outputQty <= 0) {
      setError('单次配方产出数量必须大于 0')
      return
    }
    if (lines.length === 0) {
      setError('配方必须包含至少一种原料')
      return
    }

    const payloadLines = []
    const seenItemIds = new Set<number>()

    for (let i = 0; i < lines.length; i++) {
      const line = lines[i]
      if (!line.item) {
        setError(`第 ${i + 1} 行原料未选择物料`)
        return
      }
      if (line.item.id === outputItem.id) {
        setError(`第 ${i + 1} 行原料物料与产物相同，请修改`)
        return
      }
      if (seenItemIds.has(line.item.id)) {
        setError(`物料 ${line.item.code || line.item.name} 重复添加，请合并数量`)
        return
      }
      seenItemIds.add(line.item.id)

      if (!Number.isSafeInteger(line.qty) || line.qty <= 0) {
        setError(`第 ${i + 1} 行原料用量必须大于 0`)
        return
      }

      const pctNum = parseFloat(line.lossPct) || 0
      if (pctNum < 0 || pctNum > 100) {
        setError(`第 ${i + 1} 行损耗百分比须在 0% 到 100% 之间`)
        return
      }
      const lossBps = Math.round(pctNum * 100)

      payloadLines.push({
        item_id: line.item.id,
        qty: Math.round(line.qty),
        loss_bps: lossBps,
      })
    }

    let articleIdVal: number | null = null
    if (articleIdStr.trim()) {
      const parsedArt = parseInt(articleIdStr.trim(), 10)
      if (Number.isNaN(parsedArt) || parsedArt <= 0) {
        setError('知识库文章 ID 必须为正整数')
        return
      }
      articleIdVal = parsedArt
    }

    const laborCostFen = Math.round((parseFloat(laborYuan) || 0) * 100)
    if (laborCostFen < 0) {
      setError('工费不能为负数')
      return
    }

    setBusy(true)
    try {
      if (isEdit && value) {
        const patchData: Record<string, any> = {
          name: trimmedName,
          output_item_id: outputItem.id,
          output_qty: outputQty,
          currency,
          note,
          archived,
          ...(canViewKb ? { article_id: articleIdVal || 0 } : {}),
          lines: payloadLines,
        }
        if (canEditCost) {
          patchData.labor_cost = laborCostFen
        }
        const res = await api<{ recipe: AssemblyRecipe }>(`/api/assembly/recipes/${value.id}`, {
          method: 'PATCH',
          body: patchData,
        })
        toast('配方已更新', 'ok')
        onDone(res.recipe)
      } else {
        const createData: Record<string, any> = {
          name: trimmedName,
          output_item_id: outputItem.id,
          output_qty: outputQty,
          currency,
          note,
          ...(canViewKb ? { article_id: articleIdVal || 0 } : {}),
          lines: payloadLines,
          labor_cost: canEditCost ? laborCostFen : 0,
        }
        const res = await api<{ recipe: AssemblyRecipe }>('/api/assembly/recipes', {
          method: 'POST',
          body: createData,
        })
        toast('配方已创建', 'ok')
        onDone(res.recipe)
      }
    } catch (err) {
      setError(errText(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Drawer
      title={isEdit ? `编辑配方 · ${value?.name}` : '新建加工组装配方'}
      width={560}
      onClose={() => {
        if (!busy) onClose()
      }}
      foot={
        <>
          <button className="btn btn-secondary" disabled={busy} onClick={onClose} type="button">
            取消
          </button>
          <button className="btn btn-primary grow" disabled={busy} onClick={handleSubmit} type="button">
            {busy ? '正在保存…' : isEdit ? '保存配方' : '创建配方'}
          </button>
        </>
      }
    >
      <form className="assembly-drawer-body" onSubmit={handleSubmit}>
        {error && <div className="assembly-alert">{error}</div>}

        <Field label="配方名称" req>
          <input
            className="input"
            placeholder="例如：纯银珍珠吊坠标准配方"
            value={name}
            onChange={e => setName(e.target.value)}
            maxLength={100}
            autoFocus
          />
        </Field>

        <div className="assembly-row-group">
          <Field label="产出物料 (成品或组合件)" req>
            <ItemPicker value={outputItem} onChange={setOutputItem} />
          </Field>
          <Field label="单次产出数量" req>
            <input
              className="input mono"
              type="number"
              min={1}
              value={outputQty}
              onChange={e => setOutputQty(Math.max(1, parseInt(e.target.value, 10) || 1))}
            />
          </Field>
        </div>

        <div className="assembly-row-group">
          {canEditCost ? (
            <Field label="单次配方工费 (元)">
              <input
                className="input mono"
                type="number"
                step="0.01"
                min="0"
                placeholder="0.00"
                value={laborYuan}
                onChange={e => setLaborYuan(e.target.value)}
              />
            </Field>
          ) : (
            <Field label="单次配方工费">
              <input
                className="input mono"
                disabled
                value={typeof value?.labor_cost === 'number' ? `¥${(value.labor_cost / 100).toFixed(2)}` : '无工费修改权限'}
              />
            </Field>
          )}

          <Field label="币种">
            <select
              className="input mono"
              value={currency}
              disabled={isEdit && !canEditCost}
              onChange={e => setCurrency(e.target.value)}
            >
              <option value="CNY">CNY 人民币</option>
              <option value="USD">USD 美元</option>
              <option value="TWD">TWD 新台币</option>
            </select>
          </Field>
        </div>

        {canViewKb && (
          <Field label="关联工艺文章（可选）" hint="引用知识库作业规范或质检标准">
            <input className="input" placeholder="搜索工艺文章" value={articleQuery} onChange={e => setArticleQuery(e.target.value)} />
            <select className="input" value={articleIdStr} onChange={e => setArticleIdStr(e.target.value)}>
              <option value="">不关联文章</option>
              {articleIdStr && !articles.some(a => String(a.id) === articleIdStr) && <option value={articleIdStr}>{value?.article_title || `文章 #${articleIdStr}`}</option>}
              {articles.map(a => <option value={a.id} key={a.id}>{a.title}</option>)}
            </select>
          </Field>
        )}

        <div className="assembly-lines-box">
          <div className="assembly-lines-head">
            <span>原料配件消耗清单</span>
            <button className="btn btn-secondary btn-sm" type="button" onClick={addLine}>
              <Plus />
              添加原料
            </button>
          </div>

          {lines.map((line, idx) => {
            const pct = parseFloat(line.lossPct) || 0
            const bps = Math.round(pct * 100)
            const required = calcRequiredQty(line.qty, 1, bps)

            return (
              <div key={idx} className="assembly-line-item">
                <div>
                  <div className="assembly-line-col-title">物料配件</div>
                  <ItemPicker
                    value={line.item}
                    onChange={it => updateLine(idx, { item: it })}
                  />
                </div>
                <div>
                  <div className="assembly-line-col-title">单次净用量</div>
                  <input
                    className="input mono"
                    type="number"
                    min={1}
                    value={line.qty}
                    onChange={e =>
                      updateLine(idx, { qty: Math.max(1, parseInt(e.target.value, 10) || 1) })
                    }
                  />
                </div>
                <div>
                  <div className="assembly-line-col-title">损耗率 (%)</div>
                  <div className="assembly-loss-wrap">
                    <input
                      className="input mono"
                      type="number"
                      step="0.01"
                      min="0"
                      max="100"
                      placeholder="0"
                      value={line.lossPct}
                      onChange={e => updateLine(idx, { lossPct: e.target.value })}
                    />
                    <span>%</span>
                  </div>
                </div>
                <div>
                  <div className="assembly-line-col-title">扣料预估</div>
                  <span className="assembly-preview-badge mono" title="按损耗向上取整扣料">
                    {required} {line.item?.unit || ''}
                  </span>
                </div>
                <div>
                  <div className="assembly-line-col-title">&nbsp;</div>
                  <button
                    className="btn btn-ghost btn-icon assembly-danger-btn"
                    type="button"
                    title="删除此行"
                    disabled={lines.length <= 1}
                    onClick={() => removeLine(idx)}
                  >
                    <Trash2 />
                  </button>
                </div>
              </div>
            )
          })}
        </div>

        <Field label="配方备注说明">
          <textarea
            className="textarea"
            rows={2}
            placeholder="填写加工要求、注意事项等"
            value={note}
            onChange={e => setNote(e.target.value)}
          />
        </Field>

        {isEdit && (
          <label className="setting">
            <div>
              <strong>归档此配方</strong>
              <div className="help">归档后无法据此新建组装单，历史单据不受影响</div>
            </div>
            <input
              type="checkbox"
              className="check"
              checked={archived}
              onChange={e => setArchived(e.target.checked)}
            />
          </label>
        )}
      </form>
    </Drawer>
  )
}
