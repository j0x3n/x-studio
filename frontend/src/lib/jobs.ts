import { useEffect, useRef, useState } from 'react'
import { api } from './api'
import type { Job } from './types'

export const jobRunning = (j?: Job | null) => !!j && (j.status === 'queued' || j.status === 'running')

// 盯一组后台任务：每 1.5 秒刷新一次，全部结束后停
export function useJobs(ids: number[]) {
  const [jobs, setJobs] = useState<Record<number, Job>>({})
  const key = ids.join(',')
  const live = useRef(jobs)
  live.current = jobs
  useEffect(() => {
    if (!key) { setJobs({}); return }
    let stop = false
    let timer = 0
    const list = key.split(',').map(Number)
    const tick = async () => {
      const todo = list.filter(id => !live.current[id] || jobRunning(live.current[id]))
      const got = await Promise.all(todo.map(id => api<{ job: Job }>(`/api/jobs/${id}`).then(r => r.job).catch(() => null)))
      if (stop) return
      const next = { ...live.current }
      got.forEach(j => { if (j) next[j.id] = j })
      setJobs(next)
      if (list.some(id => !next[id] || jobRunning(next[id]))) timer = window.setTimeout(tick, 1500)
    }
    tick()
    return () => { stop = true; clearTimeout(timer) }
  }, [key])
  return jobs
}
