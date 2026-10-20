import { financeTime } from '../lib/finance'
import { useState, useRef } from 'react'
import { Download, Upload, CheckCircle, FileText, Eye } from 'lucide-react'
import { api, qs } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { Drawer, Pill, Empty, Loading, Tabs } from './ui'
import { Pager, useRecord, RecordState } from './CommerceShared'
import { FinanceExplain } from './FinanceExplain'
import {
  type ReconcileRow,
  type ReconcileView,
  type ReconcileSummary,
  type ReconcileBatch,
  RECONCILE_STATUS_CONFIG,
  formatFen,
  parseReconcileCSV,
  downloadReconcileTemplate,
  newRequestKey
} from '../lib/finance'

export function FinanceReconcile() {
  const { me, can } = useAuth()
  const toast = useToast()
  const canEdit = can('finance.edit')

  const [activeTab, setActiveTab] = useState<'import' | 'history'>('import')

  // ---------- 导入与预览状态 ----------
  const [selectedShopId, setSelectedShopId] = useState<string>(() => me?.shops[0]?.id ? String(me.shops[0].id) : '')
  const [selectedCurrency, setSelectedCurrency] = useState<string>('CNY')
  const [csvText, setCsvText] = useState<string>('')
  const [parseError, setParseError] = useState<string>('')
  const [previewLoading, setPreviewLoading] = useState<boolean>(false)
  const [importing, setImporting] = useState<boolean>(false)

  // 预览结果与解析后的 rows
  const [validRows, setValidRows] = useState<ReconcileRow[] | null>(null)
  const [previewRows, setPreviewRows] = useState<ReconcileView[] | null>(null)
  const [previewSummary, setPreviewSummary] = useState<ReconcileSummary | null>(null)
  const [importRequestKey, setImportRequestKey] = useState<string>(() => newRequestKey())

  const fileInputRef = useRef<HTMLInputElement>(null)
  const previewRequest = useRef<AbortController | null>(null)

  // 当店铺、币种、CSV文本变化时，严格清空旧预览，防止拿不同数据确认
  const resetPreview = () => {
    previewRequest.current?.abort()
    setPreviewLoading(false)
    setValidRows(null)
    setPreviewRows(null)
    setPreviewSummary(null)
    setParseError('')
    setImportRequestKey(newRequestKey())
  }

  const handleShopChange = (id: string) => {
    if (importing) return
    setSelectedShopId(id)
    resetPreview()
  }

  const handleCurrencyChange = (cur: string) => {
    if (importing) return
    setSelectedCurrency(cur)
    resetPreview()
  }

  const handleTextChange = (txt: string) => {
    if (importing) return
    setCsvText(txt)
    resetPreview()
  }

  const handleFileUpload = (e: React.ChangeEvent<HTMLInputElement>) => {
    if (importing) return
    const file = e.target.files?.[0]
    if (!file) return
    if (file.size > 2 * 1024 * 1024) { toast('CSV文件最多2MB，请拆分后导入', 'error'); return }
    const reader = new FileReader()
    reader.onload = evt => {
      const content = evt.target?.result as string
      setCsvText(content || '')
      resetPreview()
    }
    reader.onerror = () => {
      toast('读取文件失败', 'error')
    }
    reader.readAsText(file)
    e.target.value = ''
  }

  const handleStartPreview = async () => {
    if (!selectedShopId) {
      toast('请先选择核对的店铺', 'error')
      return
    }
    if (!selectedCurrency) {
      toast('请先选择账单币种', 'error')
      return
    }

    const { rows, error } = parseReconcileCSV(csvText)
    if (error || !rows) {
      setParseError(error || '解析失败')
      return
    }

    setParseError('')
    previewRequest.current?.abort()
    const controller = new AbortController()
    previewRequest.current = controller
    setValidRows(null)
    setPreviewRows(null)
    setPreviewLoading(true)

    try {
      const res = await api<{
        shop_id: number
        currency: string
        rows: ReconcileView[]
        summary: ReconcileSummary
      }>('/api/finance/reconcile/preview', {
        method: 'POST',
        signal: controller.signal,
        body: {
          shop_id: Number(selectedShopId),
          currency: selectedCurrency.trim().toUpperCase(),
          rows
        }
      })

      if (controller.signal.aborted) return
      setValidRows(rows)
      setPreviewRows(res.rows)
      setPreviewSummary(res.summary)
      toast(`成功解析 ${res.rows.length} 行对账明细，差异比对已生成`, 'ok')
    } catch (e) {
      if (!controller.signal.aborted) toast(errText(e), 'error')
    } finally {
      if (previewRequest.current === controller) setPreviewLoading(false)
    }
  }

  const handleConfirmImport = async () => {
    if (importing || !validRows || !previewRows) return
    if (!canEdit) {
      toast('没有财务编辑权限，无法确认导入', 'error')
      return
    }

    setImporting(true)
    try {
      const res = await api<{
        idempotent: boolean
        batch: ReconcileBatch
        rows: ReconcileView[]
      }>('/api/finance/reconcile/import', {
        method: 'POST',
        body: {
          shop_id: Number(selectedShopId),
          currency: selectedCurrency.trim().toUpperCase(),
          request_key: importRequestKey,
          rows: validRows
        }
      })

      if (res.idempotent) {
        toast('这批账单已经导入，没有重复记录', 'info')
      } else {
        toast(`对账批次 #${res.batch.id} 导入成功，入库 ${res.batch.inserted} 行，跳过 ${res.batch.skipped} 行`, 'ok')
      }

      // 导入完成后重置并切到历史批次
      resetPreview()
      setCsvText('')
      setActiveTab('history')
      reloadHistory()
    } catch (e) {
      toast(errText(e), 'error')
    } finally {
      setImporting(false)
    }
  }

  // ---------- 历史批次状态 ----------
  const [historyShopFilter, setHistoryShopFilter] = useState<string>('')
  const [historyPage, setHistoryPage] = useState<number>(1)

  const historyQuery = qs({
    shop_id: historyShopFilter ? Number(historyShopFilter) : undefined,
    page: historyPage,
    page_size: 50
  })

  const { data: historyData, error: historyErr, reload: reloadHistory } = useRecord<{
    items: ReconcileBatch[]
    total: number
  }>(`/api/finance/reconcile${historyQuery}`)

  // 历史批次详情抽屉
  const [detailBatchId, setDetailBatchId] = useState<number | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [batchDetail, setBatchDetail] = useState<{ batch: ReconcileBatch; rows: ReconcileView[] } | null>(null)

  const openBatchDetail = async (id: number) => {
    setDetailBatchId(id)
    setDetailLoading(true)
    try {
      const res = await api<{ batch: ReconcileBatch; rows: ReconcileView[] }>(`/api/finance/reconcile/${id}`)
      setBatchDetail(res)
    } catch (e) {
      toast(errText(e), 'error')
      setDetailBatchId(null)
    } finally {
      setDetailLoading(false)
    }
  }

  return (
    <div className="finance-page">
      <div style={{ marginBottom: 4 }}>
        <Tabs
          value={activeTab}
          onChange={tab => {
            setActiveTab(tab as 'import' | 'history')
            if (tab === 'history') reloadHistory()
          }}
          items={[
            { key: 'import', label: '通用账单导入与核对' },
            { key: 'history', label: '历史对账批次快照' }
          ]}
        />
      </div>

      {activeTab === 'import' && (
        <section className="panel">
          <div className="panel-head">
            <div className="panel-title">通用 CSV 账单导入核对</div>
            <span className="grow" />
            <button className="btn btn-secondary btn-sm" onClick={downloadReconcileTemplate}>
              <Download /> 下载通用模板
            </button>
          </div>

          <div style={{ padding: '16px', display: 'flex', flexDirection: 'column', gap: 14 }}>
            {/* 顶层必要条件选择 */}
            <div className="finance-drawer-row">
              <div style={{ flex: 1 }}>
                <span className="label req">核对店铺</span>
                <select
                  className="form-control"
                  value={selectedShopId}
                  onChange={e => handleShopChange(e.target.value)}
                >
                  <option value="">请先选择核对的目标店铺</option>
                  {me?.shops.map(s => (
                    <option key={s.id} value={s.id}>{s.name}</option>
                  ))}
                </select>
              </div>

              <div style={{ width: 160 }}>
                <span className="label req">账单币种</span>
                <select
                  className="form-control mono"
                  value={selectedCurrency}
                  onChange={e => handleCurrencyChange(e.target.value)}
                >
                  <option value="CNY">CNY 人民币</option>
                  <option value="USD">USD 美元</option>
                  <option value="TWD">TWD 新台币</option>
                </select>
              </div>
            </div>

            <div className="help">
              说明：本工具仅提供通用标准化 CSV 核对（表头含：订单号、金额、平台费用、运费险、退款、结算日期；金额单位为用户元，系统自动转为整数分比对）。不宣称直接适配特定第三方平台的专用账单格式。
            </div>

            {/* CSV 输入与上传 */}
            <div className="finance-csv-box">
              <div className="inline" style={{ justifyContent: 'space-between' }}>
                <span style={{ fontSize: 13, fontWeight: 600 }}>粘贴 CSV 文本 或 上传文件</span>
                <div className="inline" style={{ gap: 8 }}>
                  <input
                    type="file"
                    ref={fileInputRef}
                    accept=".csv"
                    style={{ display: 'none' }}
                    onChange={handleFileUpload}
                  />
                  <button className="btn btn-secondary btn-sm" onClick={() => fileInputRef.current?.click()}>
                    <Upload /> 上传 .csv 文件
                  </button>
                </div>
              </div>

              <textarea
                className="finance-csv-textarea"
                rows={5}
                placeholder="订单号,金额,平台费用,运费险,退款,结算日期&#10;TB202610200001,159.00,8.50,1.50,0.00,2026-10-20&#10;…"
                value={csvText}
                onChange={e => handleTextChange(e.target.value)}
              />

              {parseError && (
                <div className="finance-alert-box">
                  {parseError}
                </div>
              )}

              <div className="inline" style={{ justifyContent: 'flex-end', gap: 8, marginTop: 4 }}>
                <button
                  className="btn btn-primary"
                  disabled={previewLoading || !csvText.trim()}
                  onClick={handleStartPreview}
                >
                  <FileText /> {previewLoading ? '正在比对…' : '解析并预览差异'}
                </button>
              </div>
            </div>

            {/* 差异比对统计与结果展示 */}
            {previewSummary && previewRows && (
              <div style={{ display: 'flex', flexDirection: 'column', gap: 12, marginTop: 8 }}>
                <div className="inline" style={{ justifyContent: 'space-between', alignItems: 'center' }}>
                  <div className="finance-summary-chips">
                    <span className="finance-summary-chip">
                      总比对行数: <span className="chip-num">{previewSummary.total}</span>
                    </span>
                    <span className="finance-summary-chip" style={{ color: 'var(--success-text)' }}>
                      成功匹配: <span className="chip-num">{previewSummary.matched}</span>
                    </span>
                    {previewSummary.difference > 0 && (
                      <span className="finance-summary-chip" style={{ color: 'var(--danger-text)', borderColor: 'var(--danger)' }}>
                        金额/退款有差异: <span className="chip-num">{previewSummary.difference}</span>
                      </span>
                    )}
                    {previewSummary.unmatched > 0 && (
                      <span className="finance-summary-chip" style={{ color: 'var(--danger-text)' }}>
                        未匹配订单: <span className="chip-num">{previewSummary.unmatched}</span>
                      </span>
                    )}
                    {previewSummary.ambiguous > 0 && (
                      <span className="finance-summary-chip" style={{ color: 'var(--warning-text)' }}>
                        单号歧义: <span className="chip-num">{previewSummary.ambiguous}</span>
                      </span>
                    )}
                    {previewSummary.currency > 0 && (
                      <span className="finance-summary-chip" style={{ color: 'var(--warning-text)' }}>
                        币种不一致: <span className="chip-num">{previewSummary.currency}</span>
                      </span>
                    )}
                    {previewSummary.canceled > 0 && (
                      <span className="finance-summary-chip">
                        订单已取消: <span className="chip-num">{previewSummary.canceled}</span>
                      </span>
                    )}
                    {previewSummary.duplicate > 0 && (
                      <span className="finance-summary-chip">
                        文件重复行: <span className="chip-num">{previewSummary.duplicate}</span>
                      </span>
                    )}
                  </div>

                  {canEdit && (
                    <button
                      className="btn btn-primary"
                      disabled={importing}
                      onClick={handleConfirmImport}
                    >
                      <CheckCircle /> {importing ? '正在确认导入…' : '确认导入对账快照'}
                    </button>
                  )}
                </div>

                <div className="finance-table-wrap">
                  <table className="table">
                    <thead>
                      <tr>
                        <th>CSV 订单号</th>
                        <th>结算日期</th>
                        <th>比对状态</th>
                        <th className="finance-col-amount">账单金额</th>
                        <th className="finance-col-amount">平台费用</th>
                        <th className="finance-col-amount">运费险</th>
                        <th className="finance-col-amount">账单退款</th>
                        <th>系统匹配单号</th>
                        <th className="finance-col-amount">系统实付</th>
                        <th className="finance-col-amount">实付差异</th>
                        <th className="finance-col-amount">退款差异</th>
                        <th>差异与说明</th>
                      </tr>
                    </thead>
                    <tbody>
                      {previewRows.map((r, i) => (
                        <tr key={i}>
                          <td><span className="mono" style={{ fontWeight: 600 }}>{r.order_no}</span></td>
                          <td className="finance-col-date">{r.settled_at}</td>
                          <td>
                            <Pill kind={RECONCILE_STATUS_CONFIG[r.status]?.pill || 'pill-neutral'}>
                              {RECONCILE_STATUS_CONFIG[r.status]?.label || r.status}
                            </Pill>
                          </td>
                          <td className="finance-col-amount">{formatFen(r.amount, selectedCurrency)}</td>
                          <td className="finance-col-amount">{formatFen(r.fee, selectedCurrency)}</td>
                          <td className="finance-col-amount">{formatFen(r.shipping_insurance, selectedCurrency)}</td>
                          <td className="finance-col-amount">{formatFen(r.refund, selectedCurrency)}</td>
                          <td>
                            {r.system_no ? (
                              <span className="mono">{r.system_no}</span>
                            ) : (
                              <span className="help">—</span>
                            )}
                          </td>
                          <td className="finance-col-amount">
                            {r.system_amount !== null ? formatFen(r.system_amount, selectedCurrency) : '—'}
                          </td>
                          <td className="finance-col-amount">
                            {r.diff_amount !== null && r.diff_amount !== 0 ? (
                              <span className="finance-diff-badge danger">
                                {r.diff_amount > 0 ? '+' : ''}{formatFen(r.diff_amount, selectedCurrency)}
                              </span>
                            ) : r.diff_amount === 0 ? (
                              <span className="finance-diff-badge success">一致</span>
                            ) : '—'}
                          </td>
                          <td className="finance-col-amount">
                            {r.diff_refund !== null && r.diff_refund !== 0 ? (
                              <span className="finance-diff-badge danger">
                                {r.diff_refund > 0 ? '+' : ''}{formatFen(r.diff_refund, selectedCurrency)}
                              </span>
                            ) : r.diff_refund === 0 ? (
                              <span className="finance-diff-badge success">一致</span>
                            ) : '—'}
                          </td>
                          <td style={{ maxWidth: 200, fontSize: 12, color: 'var(--text-2)' }}>
                            {r.reason || '无'}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            )}
          </div>
        </section>
      )}

      {activeTab === 'history' && (
        <section className="panel">
          <div className="panel-head">
            <div className="panel-title">历史对账快照批次</div>
            <span className="grow" />
          </div>

          <div className="finance-toolbar">
            <span className="finance-stat-label">店铺范围：</span>
            <select
              value={historyShopFilter}
              onChange={e => { setHistoryShopFilter(e.target.value); setHistoryPage(1) }}
            >
              <option value="">全部店铺</option>
              {me?.shops.map(s => (
                <option key={s.id} value={s.id}>{s.name}</option>
              ))}
            </select>
          </div>

          {historyErr ? (
            <RecordState error={historyErr} reload={reloadHistory} />
          ) : !historyData ? (
            <Loading />
          ) : historyData.items.length === 0 ? (
            <Empty title="暂无历史对账快照" desc="切换到“通用账单导入与核对”标签页即可上传并持久化对账快照" />
          ) : (
            <div className="finance-table-wrap">
              <table className="table">
                <thead>
                  <tr>
                    <th>批次编号</th>
                    <th>店铺</th>
                    <th>币种</th>
                    <th className="finance-col-amount">账单总行数</th>
                    <th className="finance-col-amount">已入库行数</th>
                    <th className="finance-col-amount">跳过重复数</th>
                    <th className="finance-col-amount">成功匹配数</th>
                    <th>导入时间</th>
                    <th>操作</th>
                  </tr>
                </thead>
                <tbody>
                  {historyData.items.map(b => (
                    <tr key={b.id}>
                      <td><span className="mono" style={{ fontWeight: 600 }}>#{b.id}</span></td>
                      <td>{b.shop_name}</td>
                      <td><span className="mono">{b.currency}</span></td>
                      <td className="finance-col-amount">{b.row_count}</td>
                      <td className="finance-col-amount" style={{ color: 'var(--success-text)' }}>{b.inserted}</td>
                      <td className="finance-col-amount" style={{ color: 'var(--text-3)' }}>{b.skipped}</td>
                      <td className="finance-col-amount" style={{ fontWeight: 600 }}>{b.matched}</td>
                      <td className="finance-col-date">
                        {financeTime(b.created_at)}
                      </td>
                      <td>
                        <button className="btn btn-ghost btn-sm" onClick={() => openBatchDetail(b.id)}>
                          <Eye /> 查看快照详情
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          {historyData && historyData.total > 50 && (
            <div style={{ padding: '8px 16px' }}>
              <Pager total={historyData.total} page={historyPage} setPage={setHistoryPage} size={50} />
            </div>
          )}
        </section>
      )}

      {/* 历史批次快照详情抽屉 */}
      {detailBatchId !== null && (
        <Drawer
          title={`对账批次快照明细 #${detailBatchId}`}
          width={840}
          onClose={() => { setDetailBatchId(null); setBatchDetail(null) }}
        >
          {detailLoading ? (
            <Loading />
          ) : !batchDetail ? (
            <Empty title="无法读取批次详情" />
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
              {/* 批次概览信息 */}
              <div className="finance-stat-grid" style={{ gridTemplateColumns: 'repeat(4, 1fr)' }}>
                <div className="finance-stat-card">
                  <span className="finance-stat-label">店铺</span>
                  <span style={{ fontSize: 14, fontWeight: 600 }}>{batchDetail.batch.shop_name}</span>
                </div>
                <div className="finance-stat-card">
                  <span className="finance-stat-label">币种</span>
                  <span className="mono" style={{ fontSize: 14, fontWeight: 600 }}>{batchDetail.batch.currency}</span>
                </div>
                <div className="finance-stat-card">
                  <span className="finance-stat-label">入库 / 跳过</span>
                  <span className="mono" style={{ fontSize: 14, fontWeight: 600 }}>
                    {batchDetail.batch.inserted} / {batchDetail.batch.skipped}
                  </span>
                </div>
                <div className="finance-stat-card">
                  <span className="finance-stat-label">成功匹配</span>
                  <span className="mono" style={{ fontSize: 14, fontWeight: 600, color: 'var(--success-text)' }}>
                    {batchDetail.batch.matched}
                  </span>
                </div>
              </div>

              {/* 人工调用 AI 解释差异组件 */}
              <div style={{ background: 'var(--surface-2)', padding: 12, borderRadius: 8 }}>
                <FinanceExplain batchId={batchDetail.batch.id} />
              </div>

              {/* 落库行明细快照 */}
              <div>
                <h4 style={{ fontSize: 13, fontWeight: 600, marginBottom: 8 }}>
                  持久化账单行记录 ({batchDetail.rows.length} 行)
                </h4>
                <div className="finance-table-wrap">
                  <table className="table">
                    <thead>
                      <tr>
                        <th>订单号</th>
                        <th>结算日期</th>
                        <th>状态</th>
                        <th className="finance-col-amount">账单金额</th>
                        <th className="finance-col-amount">费用/险</th>
                        <th className="finance-col-amount">退款</th>
                        <th>系统匹配单号</th>
                        <th className="finance-col-amount">实付差异</th>
                        <th className="finance-col-amount">退款差异</th>
                        <th>说明原因</th>
                      </tr>
                    </thead>
                    <tbody>
                      {batchDetail.rows.map((row, idx) => (
                        <tr key={idx}>
                          <td><span className="mono" style={{ fontWeight: 600 }}>{row.order_no}</span></td>
                          <td className="finance-col-date">{row.settled_at}</td>
                          <td>
                            <Pill kind={RECONCILE_STATUS_CONFIG[row.status]?.pill || 'pill-neutral'}>
                              {RECONCILE_STATUS_CONFIG[row.status]?.label || row.status}
                            </Pill>
                          </td>
                          <td className="finance-col-amount">{formatFen(row.amount, batchDetail.batch.currency)}</td>
                          <td className="finance-col-amount">
                            {formatFen(row.fee + row.shipping_insurance, batchDetail.batch.currency)}
                          </td>
                          <td className="finance-col-amount">{formatFen(row.refund, batchDetail.batch.currency)}</td>
                          <td>
                            {row.system_no ? (
                              <span className="mono">{row.system_no}</span>
                            ) : (
                              <span className="help">—</span>
                            )}
                          </td>
                          <td className="finance-col-amount">
                            {row.diff_amount !== null && row.diff_amount !== 0 ? (
                              <span className="finance-diff-badge danger">
                                {row.diff_amount > 0 ? '+' : ''}{formatFen(row.diff_amount, batchDetail.batch.currency)}
                              </span>
                            ) : row.diff_amount === 0 ? (
                              <span className="finance-diff-badge success">一致</span>
                            ) : '—'}
                          </td>
                          <td className="finance-col-amount">
                            {row.diff_refund !== null && row.diff_refund !== 0 ? (
                              <span className="finance-diff-badge danger">
                                {row.diff_refund > 0 ? '+' : ''}{formatFen(row.diff_refund, batchDetail.batch.currency)}
                              </span>
                            ) : row.diff_refund === 0 ? (
                              <span className="finance-diff-badge success">一致</span>
                            ) : '—'}
                          </td>
                          <td style={{ maxWidth: 180, fontSize: 12, color: 'var(--text-2)' }}>
                            {row.reason || '—'}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            </div>
          )}
        </Drawer>
      )}
    </div>
  )
}
