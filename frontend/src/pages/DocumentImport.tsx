import { useEffect, useState, useRef, type ChangeEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { FileImage, AlertTriangle, CheckCircle, Plus, Trash2, ArrowLeft, RefreshCw, ShoppingCart } from 'lucide-react'
import { api, type Page } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { toFen, fenToInput, type InvItem } from '../lib/inventory'
import { money, type Supplier } from '../lib/commerce'
import { Topbar, Empty, Field } from '../components/ui'
import { useRecord } from '../components/CommerceShared'
import { ItemPicker } from '../components/InventoryForms'
import '../styles/intake.css'

type RecognizedCandidate = {
  id: number
  code: string
  name: string
  unit: string
}

type RecognizedLine = {
  code: string
  name: string
  qty: number
  price: number // in cents
  item_id: number | null
  candidates: RecognizedCandidate[]
}

type RecognizeResponse = {
  supplier_id: number | null
  supplier_name: string
  expected_at: string
  currency: string
  shipping_fee: number // in cents
  lines: RecognizedLine[]
  warnings: string[]
}

type EditableLine = {
  item: InvItem | null
  item_id: number
  code: string
  name: string
  qty: string
  price: string // in yuan
  candidates: RecognizedCandidate[]
  pickerOpen?: boolean
}

export function DocumentImport() {
  const { can } = useAuth()
  const nav = useNavigate()
  const toast = useToast()
  const fileInputRef = useRef<HTMLInputElement>(null)

  const { data: suppliers } = useRecord<Page<Supplier>>('/api/suppliers?archived=1&page_size=200')

  // 上传与预览状态
  const [file, setFile] = useState<File | null>(null)
  const [previewURL, setPreviewURL] = useState<string>('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  // 识别结果编辑状态
  const [recognized, setRecognized] = useState<RecognizeResponse | null>(null)
  const [supplierID, setSupplierID] = useState<number>(0)
  const [currency, setCurrency] = useState('CNY')
  const [shippingFee, setShippingFee] = useState('0')
  const [expectedAt, setExpectedAt] = useState('')
  const [note, setNote] = useState('单据智能识别录入')
  const [lines, setLines] = useState<EditableLine[]>([])
  const [createBusy, setCreateBusy] = useState(false)
  useEffect(() => () => { if (previewURL) URL.revokeObjectURL(previewURL) }, [previewURL])

  if (!can('purchase.manage')) {
    return (
      <>
        <Topbar title="单据识别" />
        <Empty title="没有管理采购的权限" desc="请联系管理员开通 purchase.manage 权限。" />
      </>
    )
  }

  const handleFileChange = (e: ChangeEvent<HTMLInputElement>) => {
    const selected = e.target.files?.[0]
    if (!selected) return
    if (!selected.type.startsWith('image/')) {
      setError('只支持 JPG、PNG、WebP 等图片格式文件')
      return
    }
    if (selected.size > 25 * 1024 * 1024) {
      setError('单张图片大小不能超过 25 MB')
      return
    }
    setFile(selected)
    setError('')
    const url = URL.createObjectURL(selected)
    setPreviewURL(url)
    setRecognized(null)
  }

  const handleRecognize = async () => {
    if (!file || busy) return
    setBusy(true)
    setError('')

    const formData = new FormData()
    formData.append('file', file)

    try {
      const res = await api<RecognizeResponse>('/api/purchase-orders/recognize', {
        form: formData,
      })
      setRecognized(res)
      setSupplierID(res.supplier_id || 0)
      setCurrency(res.currency || 'CNY')
      setShippingFee(fenToInput(res.shipping_fee ?? 0))
      setExpectedAt(res.expected_at || '')

      const editLines: EditableLine[] = res.lines.map(l => ({
        item: l.item_id ? ({ id: l.item_id, code: l.code, name: l.name } as InvItem) : null,
        item_id: l.item_id || 0,
        code: l.code,
        name: l.name,
        qty: String(l.qty || 1),
        price: fenToInput(l.price ?? 0),
        candidates: l.candidates || [],
      }))
      setLines(editLines)
      toast('单据识别完成，请核对信息', 'ok')
    } catch (e) {
      setError(errText(e))
    } finally {
      setBusy(false)
    }
  }

  const updateLine = (i: number, patch: Partial<EditableLine>) => {
    setLines(ls => ls.map((l, idx) => (idx === i ? { ...l, ...patch } : l)))
  }

  const removeLine = (i: number) => {
    setLines(ls => ls.filter((_, idx) => idx !== i))
  }

  const addLine = () => {
    setLines(ls => [
      ...ls,
      {
        item: null,
        item_id: 0,
        code: '',
        name: '',
        qty: '1',
        price: '0.00',
        candidates: [],
        pickerOpen: true,
      },
    ])
  }

  const selectCandidate = (i: number, cand: RecognizedCandidate) => {
    updateLine(i, {
      item_id: cand.id,
      item: { id: cand.id, code: cand.code, name: cand.name, unit: cand.unit } as InvItem,
      code: cand.code,
      name: cand.name,
      pickerOpen: false,
    })
  }

  const totalGoods = lines.reduce(
    (sum, l) => sum + Math.round(Number(l.qty || 0) * toFen(l.price || '0')),
    0
  )
  const totalOrder = totalGoods + toFen(shippingFee)

  const isFormValid =
    supplierID > 0 &&
    lines.length > 0 &&
    lines.every(
      l =>
        l.item_id > 0 &&
        Number.isSafeInteger(Number(l.qty)) &&
        Number(l.qty) > 0 &&
        Number(l.price) >= 0 && Number.isSafeInteger(Math.round(Number(l.qty) * toFen(l.price)))
    )

  const handleCreateDraft = async () => {
    if (!isFormValid || createBusy) return
    setCreateBusy(true)
    setError('')

    try {
      const merged = new Map<number, { item_id: number; qty: number; amount: number }>()
      for (const line of lines) {
        const row = merged.get(line.item_id) || { item_id: line.item_id, qty: 0, amount: 0 }
        row.qty += Number(line.qty)
        row.amount += Math.round(Number(line.qty) * toFen(line.price))
        if (!Number.isSafeInteger(row.qty) || !Number.isSafeInteger(row.amount)) throw new Error('数量或金额过大，请拆分录入')
        merged.set(row.item_id, row)
      }
      const payload = {
        supplier_id: supplierID,
        currency,
        shipping_fee: toFen(shippingFee),
        expected_at: expectedAt,
        note,
        lines: [...merged.values()],
      }
      const res = await api<{ id: number }>('/api/purchase-orders', {
        method: 'POST',
        body: payload,
      })
      toast('采购草稿已生成', 'ok')
      nav(`/purchase-orders/${res.id}`)
    } catch (e) {
      setError(errText(e))
    } finally {
      setCreateBusy(false)
    }
  }

  return (
    <>
      <Topbar title="单据识别" sub="上传送货单、发票或采购清单图片，智能解析并生成采购草稿">
        <Link className="btn btn-secondary" to="/purchase-orders">
          <ShoppingCart />
          采购单列表
        </Link>
      </Topbar>

      <div className="scroll">
        <main className="content">
          {!recognized ? (
            <div className="panel intake-upload-panel">
              <div className="panel-title intake-mb">上传采购单据图片</div>
              <div
                className="recognize-upload-box"
                onClick={() => fileInputRef.current?.click()}
                onDragOver={e => e.preventDefault()}
                onDrop={e => {
                  e.preventDefault()
                  if (e.dataTransfer.files?.[0]) {
                    const dropped = e.dataTransfer.files[0]
                    if (!dropped.type.startsWith('image/')) {
                      setError('只支持 JPG、PNG、WebP 等图片格式文件')
                      return
                    }
                    setFile(dropped)
                    setError('')
                    setPreviewURL(URL.createObjectURL(dropped))
                  }
                }}
              >
                <input
                  ref={fileInputRef}
                  type="file"
                  accept="image/jpeg,image/png,image/webp"
                  onChange={handleFileChange}
                />
                <FileImage className="recognize-upload-icon" />
                <div className="intake-medium intake-mb-sm">
                  {file ? file.name : '点击或将单据图片拖拽到此处'}
                </div>
                <div className="t3 fs-12">
                  支持 JPG、PNG、WebP 格式单张图片，最大 25 MB
                </div>
                {file && (
                  <div className="t2 fs-12 mt-2">
                    文件大小：{(file.size / 1024 / 1024).toFixed(2)} MB
                  </div>
                )}
              </div>

              {previewURL && (
                <div className="recognize-image-wrap intake-mt">
                  <img src={previewURL} alt="单据预览" />
                </div>
              )}

              {error && <div className="form-err mt-3" role="alert">{error}</div>}

              <div className="inline intake-end intake-gap intake-mt">
                {file && (
                  <button
                    className="btn btn-primary"
                    disabled={busy}
                    onClick={handleRecognize}
                  >
                    {busy ? '正在智能识别…' : '开始单据识别'}
                  </button>
                )}
              </div>
            </div>
          ) : (
            <div className="recognize-grid">
              {/* 左侧单据原图 */}
              <div className="recognize-preview-pane">
                <div className="inline intake-between">
                  <span className="intake-medium fs-13">单据原图</span>
                  <button
                    className="btn btn-ghost btn-sm"
                    disabled={busy}
                    onClick={() => {
                      setRecognized(null)
                    }}
                  >
                    <ArrowLeft size={14} /> 重新选择
                  </button>
                </div>
                <div className="recognize-image-wrap">
                  <img src={previewURL} alt="单据原图" />
                </div>
                <button
                  className="btn btn-secondary btn-sm"
                  disabled={busy}
                  onClick={handleRecognize}
                >
                  <RefreshCw size={14} /> 重新识别
                </button>
              </div>

              {/* 右侧识别结果编辑确认 */}
              <div className="recognize-result-pane">
                {recognized.warnings && recognized.warnings.length > 0 && (
                  <div className="recognize-warnings" role="alert">
                    <div className="recognize-warning-title">
                      <AlertTriangle size={15} /> 识别提示与人工核对事项
                    </div>
                    <ul className="recognize-warning-list">
                      {recognized.warnings.map((w, idx) => (
                        <li key={idx}>{w}</li>
                      ))}
                    </ul>
                  </div>
                )}

                <div className="panel">
                  <div className="panel-head">
                    <div className="panel-title">基本单据信息</div>
                  </div>
                  <div className="form-row">
                    <Field label="供应商" req hint="系统已根据识别结果匹配，可重新选择">
                      <select
                        className="input"
                        value={supplierID}
                        onChange={e => setSupplierID(Number(e.target.value))}
                      >
                        <option value={0}>请选择供应商</option>
                        {recognized.supplier_name && !supplierID && (
                          <option value={0} disabled>
                            未匹配到：{recognized.supplier_name}
                          </option>
                        )}
                        {suppliers?.items.map(s => (
                          <option key={s.id} value={s.id}>
                            {s.name}
                          </option>
                        ))}
                      </select>
                    </Field>

                    <Field label="预计到货日期">
                      <input
                        className="input mono"
                        type="date"
                        value={expectedAt}
                        onChange={e => setExpectedAt(e.target.value)}
                      />
                    </Field>
                  </div>

                  <div className="form-row">
                    <Field label="币种">
                      <input
                        className="input mono"
                        maxLength={3}
                        value={currency}
                        onChange={e => setCurrency(e.target.value.toUpperCase())}
                      />
                    </Field>

                    <Field label="运费（元）">
                      <input
                        className="input mono"
                        type="number"
                        min="0"
                        step="0.01"
                        value={shippingFee}
                        onChange={e => setShippingFee(e.target.value)}
                      />
                    </Field>
                  </div>
                </div>

                <div className="panel">
                  <div className="panel-head">
                    <div className="panel-title">物料明细（{lines.length}）</div>
                    <span className="grow" />
                    <button className="btn btn-secondary btn-sm" onClick={addLine}>
                      <Plus size={14} /> 添加一行
                    </button>
                  </div>

                  <div className="table-wrap">
                    <table className="table recognize-lines-table">
                      <thead>
                        <tr>
                          <th>物料匹配</th>
                          <th style={{ width: 100 }}>数量</th>
                          <th style={{ width: 110 }}>单价（元）</th>
                          <th className="num" style={{ width: 110 }}>小计</th>
                          <th style={{ width: 40 }}></th>
                        </tr>
                      </thead>
                      <tbody>
                        {lines.map((l, i) => {
                          const sub = Math.round(Number(l.qty || 0) * toFen(l.price || '0'))
                          const isMatched = l.item_id > 0
                          return (
                            <tr key={i}>
                              <td>
                                {isMatched && !l.pickerOpen ? (
                                  <div>
                                    <div className="inline intake-gap-sm">
                                      <CheckCircle size={14} className="intake-success" />
                                      <span className="mono intake-medium">{l.code}</span>
                                      <span className="t2">{l.name}</span>
                                      <button
                                        className="btn btn-ghost btn-sm py-0 h-6 fs-11"
                                        onClick={() => updateLine(i, { pickerOpen: true })}
                                      >
                                        更改
                                      </button>
                                    </div>
                                  </div>
                                ) : (
                                  <div className="col intake-gap-sm">
                                    <div className="t2 fs-12">
                                      识别文本：<strong>{l.name || l.code || '未命名'}</strong>
                                    </div>
                                    {l.candidates.length > 0 && !l.pickerOpen && (
                                      <div className="inline intake-gap-sm intake-wrap">
                                        <span className="t3 fs-11">匹配候选：</span>
                                        {l.candidates.map(cand => (
                                          <button
                                            key={cand.id}
                                            type="button"
                                            className="candidate-badge cursor-pointer"
                                            onClick={() => selectCandidate(i, cand)}
                                          >
                                            {cand.code} · {cand.name}
                                          </button>
                                        ))}
                                        <button
                                          type="button"
                                          className="btn btn-ghost btn-sm py-0 h-5 fs-11"
                                          onClick={() => updateLine(i, { pickerOpen: true })}
                                        >
                                          搜索其他
                                        </button>
                                      </div>
                                    )}
                                    {l.pickerOpen && (
                                      <div className="intake-mt-sm">
                                        <ItemPicker
                                          value={l.item}
                                          onChange={it => {
                                            if (it) {
                                              updateLine(i, {
                                                item_id: it.id,
                                                item: it,
                                                code: it.code,
                                                name: it.name,
                                                pickerOpen: false,
                                              })
                                            }
                                          }}
                                        />
                                      </div>
                                    )}
                                  </div>
                                )}
                              </td>
                              <td>
                                <input
                                  className="input mono"
                                  type="number"
                                  min="1"
                                  step="1"
                                  value={l.qty}
                                  onChange={e => updateLine(i, { qty: e.target.value })}
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
                                />
                              </td>
                              <td className="num mono intake-medium">
                                {money(sub, currency)}
                              </td>
                              <td>
                                <button
                                  className="btn btn-ghost btn-icon btn-sm"
                                  title="移除"
                                  onClick={() => removeLine(i)}
                                >
                                  <Trash2 size={14} />
                                </button>
                              </td>
                            </tr>
                          )
                        })}
                      </tbody>
                    </table>
                  </div>

                  <Field label="采购备注">
                    <textarea
                      className="textarea"
                      rows={2}
                      maxLength={1000}
                      value={note}
                      onChange={e => setNote(e.target.value)}
                    />
                  </Field>

                  {error && <div className="form-err mt-2" role="alert">{error}</div>}

                  <div className="recognize-foot">
                    <div className="t2 mono fs-13">
                      货款合计 {money(totalGoods, currency)} + 运费 {money(toFen(shippingFee), currency)} = <strong>{money(totalOrder, currency)}</strong>
                    </div>
                    <button
                      className="btn btn-primary"
                      disabled={!isFormValid || createBusy}
                      onClick={handleCreateDraft}
                    >
                      {createBusy ? '正在生成草稿…' : '确认生成采购草稿'}
                    </button>
                  </div>
                </div>
              </div>
            </div>
          )}
        </main>
      </div>
    </>
  )
}
