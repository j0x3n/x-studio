import { useEffect, useState } from 'react'
import { Check, ChevronLeft, ChevronRight, ImageOff, Search } from 'lucide-react'
import { api, qs, type Page } from '../lib/api'
import type { Asset, Job } from '../lib/types'
import { useJobs, jobRunning } from '../lib/jobs'
import { assetTitle, imgUrl } from '../lib/format'
import { useToast, errText } from '../lib/toast'
import { Drawer, Empty, Loading } from './ui'

export function TryOnAssetPicker({ onClose, onPick, productId }: { onClose: () => void; onPick: (items: Asset[]) => void; productId?: number }) {
  const toast = useToast()
  const [q, setQ] = useState('')
  const [page, setPage] = useState(1)
  const [data, setData] = useState<Page<Asset> | null>(null)
  const [error, setError] = useState('')
  const [selected, setSelected] = useState<Asset[]>([])
  useEffect(() => {
    const ctrl = new AbortController()
    setData(null); setError('')
    const timer = setTimeout(() => api<Page<Asset>>('/api/assets' + qs({ q, product_id: productId, kind: 'photo', page, page_size: 30 }), { signal: ctrl.signal })
      .then(setData).catch(e => { if (!ctrl.signal.aborted) setError(errText(e)) }), 200)
    return () => { clearTimeout(timer); ctrl.abort() }
  }, [q, page, productId])
  const toggle = (a: Asset) => {
    if (selected.some(s => s.id === a.id)) { setSelected(s => s.filter(v => v.id !== a.id)); return }
    if (selected.length >= 2) { toast('一次选择 1 张主图和最多 1 张细节图', 'error'); return }
    if (selected[0] && selected[0].product_id !== a.product_id) { toast('主图和细节图需要属于同一款商品', 'error'); return }
    setSelected(s => [...s, a])
  }
  return <Drawer title="选择商品图片" width={720} onClose={onClose} foot={<>
    <span className="help grow">先选主图，再选细节图 · 已选 {selected.length} / 2</span>
    <button className="btn btn-secondary" onClick={onClose}>取消</button>
    <button className="btn btn-primary" disabled={!selected.length} onClick={() => onPick(selected)}>使用 {selected.length} 张</button>
  </>}>
    <label className="field"><Search /><input autoFocus placeholder="搜索商品、标题、标签" value={q} onChange={e => { setQ(e.target.value); setPage(1) }} /></label>
    {error ? <Empty title="图片加载失败" desc={error} /> : !data ? <Loading /> : !data.items.length ? <Empty title="没有找到商品图" desc="换个关键词，或先在商品页上传图片。" /> : <div className="model-picker-grid">
      {data.items.map(a => { const index = selected.findIndex(s => s.id === a.id); return <button key={a.id} className={'model-pick' + (index >= 0 ? ' selected' : '')} onClick={() => toggle(a)}>
        <img src={imgUrl(a, 240)} alt={assetTitle(a)} loading="lazy" />
        {index >= 0 && <span className="model-pick-mark"><Check />{index === 0 ? '主图' : '细节'}</span>}
        <span className="model-pick-name ellipsis">{assetTitle(a)}</span><span className="help ellipsis">{a.product?.name || `商品 #${a.product_id}`}</span>
      </button> })}
    </div>}
    {data && data.total > 30 && <div className="pager"><button className="btn btn-secondary btn-sm" aria-label="上一页" disabled={page <= 1} onClick={() => setPage(p => p - 1)}><ChevronLeft /></button><span className="mono">{page} / {Math.ceil(data.total / 30)}</span><button className="btn btn-secondary btn-sm" aria-label="下一页" disabled={page * 30 >= data.total} onClick={() => setPage(p => p + 1)}><ChevronRight /></button></div>}
  </Drawer>
}

export function ModelImage({ src, alt = '' }: { src?: string; alt?: string }) {
  return src ? <img src={src} alt={alt} loading="lazy" /> : <span className="model-image-empty"><ImageOff /><span>待添加参考图</span></span>
}

// 使用现有任务轮询；从工作台的活动任务恢复，避免刷新后重复提交。
export function useTryOnJob(enabled: boolean) {
  const [jobId, setJobId] = useState<number | null>(null)
  const [initial, setInitial] = useState<Job | null>(null)
  const jobs = useJobs(jobId ? [jobId] : [])
  useEffect(() => {
    if (!enabled) return
    let canceled = false
    api<{ job: Job | null }>('/api/tryon/state').then(r => {
      if (!canceled && r.job) { setInitial(old => old || r.job); setJobId(old => old ?? r.job!.id) }
    }).catch(() => {})
    return () => { canceled = true }
  }, [enabled])
  const job = jobId ? jobs[jobId] || (initial?.id === jobId ? initial : null) : null
  const start = (j: Job) => { setInitial(j); setJobId(j.id) }
  const running = !!jobId && (!job || jobRunning(job))
  return { job, running, start }
}

export function ModelJobStatus({ job, running }: { job: Job | null; running: boolean }) {
  if (!job) return null
  return <div className={'model-job' + (job.status === 'failed' ? ' failed' : '')} role="status">
    <span className="mono">{running ? `${Math.round(job.progress || 0)}%` : job.status === 'failed' ? '失败' : '完成'}</span>
    <span>{job.message || (job.status === 'queued' ? '已排队，等待生成' : running ? 'AI 正在处理，离开页面后仍会继续' : job.status === 'failed' ? '任务未完成，可调整后重试' : '处理完成')}</span>
  </div>
}
