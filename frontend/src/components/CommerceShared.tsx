import { useEffect, useState, type ReactNode } from 'react'
import { api } from '../lib/api'
import { errText } from '../lib/toast'
import { Empty, Loading } from './ui'

export function useRecord<T>(path: string) {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState('')
  const [stamp, setStamp] = useState(0)
  useEffect(() => {
    let active = true
    setData(null); setError('')
    api<T>(path).then(r => { if (active) setData(r) }).catch(e => { if (active) setError(errText(e)) })
    return () => { active = false }
  }, [path, stamp])
  return { data, error, reload: () => setStamp(s => s + 1) }
}

export function RecordState({ error, reload }: { error: string; reload: () => void }) {
  return error ? <Empty title="读取失败" desc={error}><button className="btn btn-secondary" onClick={reload}>重试</button></Empty> : <Loading />
}

export function Pager({ total, page, setPage, size = 50 }: { total: number; page: number; setPage: (n: number) => void; size?: number }) {
  const pages = Math.max(1, Math.ceil(total / size))
  return <div className="pager">共 <span className="mono">{total}</span> 条<span className="spacer" /><button className="btn btn-ghost btn-sm" disabled={page <= 1} onClick={() => setPage(page - 1)}>上一页</button><span className="mono">{page} / {pages}</span><button className="btn btn-ghost btn-sm" disabled={page >= pages} onClick={() => setPage(page + 1)}>下一页</button></div>
}

export function DocumentPanel({ title, children, action }: { title: string; children: ReactNode; action?: ReactNode }) {
  return <section className="panel"><div className="panel-head"><div className="panel-title">{title}</div><span className="grow" />{action}</div>{children}</section>
}
