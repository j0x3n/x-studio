import { useState } from 'react'
import { Sparkles } from 'lucide-react'
import { api } from '../lib/api'
import { errText } from '../lib/toast'

export function FinanceExplain({ batchId }: { batchId: number }) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [result, setResult] = useState<{ explanation: string; checks: string[] } | null>(null)
  const explain = async () => {
    if (busy) return
    setBusy(true); setError('')
    try { setResult(await api<{ explanation: string; checks: string[] }>(`/api/finance/reconcile/${batchId}/explain`, { method: 'POST' })) }
    catch (e) { setError(errText(e)) } finally { setBusy(false) }
  }
  return <div className="stack" style={{ gap: 8 }}>
    <div className="inline"><button className="btn btn-secondary btn-sm" disabled={busy} onClick={explain}><Sparkles />{busy ? '正在解释…' : 'AI 解释差异'}</button><span className="help">消耗一次文本额度，仅提供核对建议</span></div>
    {error && <p className="help" role="alert">{error}</p>}
    {result && <div className="panel panel-pad"><p style={{ whiteSpace: 'pre-wrap', margin: 0 }}>{result.explanation}</p>{result.checks.length > 0 && <ul>{result.checks.map((check, i) => <li key={i}>{check}</li>)}</ul>}</div>}
  </div>
}
