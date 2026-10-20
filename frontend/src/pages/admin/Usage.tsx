import { useEffect, useState } from 'react'
import { api, qs, type Page } from '../../lib/api'
import type { UserRow } from '../../lib/types'
import { Loading, Empty } from '../../components/ui'

type Row = { user_id?: number; feature?: string; kind: string; units: number; est_cost: number }
const FEATURE: Record<string, string> = { classify: '分拣', copy: '写文案', copy_rewrite: '文案改写', copy_check: '合规审查', enhance: '高清化', fill: '补图', process: '图片处理', tag: '打标签', assistant: 'AI 助手', upscale: '图片处理·高清化', model_create: '模特创建与参考图', tryon_analyze: '试穿商品识别', tryon: '模特穿戴出图', document_analyze: '单据识别', finance_explain: '对账差异解释' }

const thisMonth = () => { const d = new Date(); return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}` }

export function UsageTab() {
  const [month, setMonth] = useState(thisMonth())
  const [group, setGroup] = useState<'user' | 'feature'>('user')
  const [rows, setRows] = useState<Row[] | null>(null)
  const [users, setUsers] = useState<UserRow[]>([])
  useEffect(() => { api<Page<UserRow>>('/api/users?page_size=200').then(r => setUsers(r.items)).catch(() => {}) }, [])
  useEffect(() => {
    setRows(null)
    api<{ items: Row[] }>('/api/usage' + qs({ month, group })).then(r => setRows(r.items || [])).catch(() => setRows([]))
  }, [month, group])

  // 同一个人 / 功能的图片和文本合成一行
  const merged = new Map<string, { name: string; image: number; text: number; cost: number }>()
  for (const r of rows || []) {
    const k = String(group === 'user' ? r.user_id : r.feature)
    const name = group === 'user' ? (users.find(u => u.id === r.user_id)?.display_name || users.find(u => u.id === r.user_id)?.username || `#${r.user_id}`) : (FEATURE[r.feature || ''] || r.feature || '未标记')
    const m = merged.get(k) || { name, image: 0, text: 0, cost: 0 }
    if (r.kind === 'image') m.image += r.units; else m.text += r.units
    m.cost += r.est_cost || 0
    merged.set(k, m)
  }
  const list = [...merged.values()].sort((a, b) => b.cost - a.cost || b.image - a.image)
  const total = list.reduce((a, b) => ({ image: a.image + b.image, text: a.text + b.text, cost: a.cost + b.cost }), { image: 0, text: 0, cost: 0 })
  return (
    <section className="panel">
      <div className="panel-head">
        <div className="titles"><div className="panel-title">AI 用量</div><div className="panel-sub">只算调用成功的；费用按「设置」里填的单价估算</div></div>
        <span className="grow" />
        <div className="segmented"><button className={group === 'user' ? 'active' : ''} onClick={() => setGroup('user')}>按人</button><button className={group === 'feature' ? 'active' : ''} onClick={() => setGroup('feature')}>按功能</button></div>
        <input className="input mono" type="month" style={{ width: 150 }} value={month} onChange={e => setMonth(e.target.value)} />
      </div>
      {!rows ? <Loading /> : list.length === 0 ? <Empty title="这个月还没有 AI 调用" /> : (
        <div className="table-wrap"><table className="table">
          <thead><tr><th>{group === 'user' ? '账号' : '功能'}</th><th className="num">生图（张）</th><th className="num">文本（次）</th><th className="num">估算费用</th></tr></thead>
          <tbody>
            {list.map(r => <tr key={r.name}><td>{r.name}</td><td className="num mono">{r.image}</td><td className="num mono">{r.text}</td><td className="amount">¥{r.cost.toFixed(2)}</td></tr>)}
            <tr><td className="t3">合计</td><td className="num mono">{total.image}</td><td className="num mono">{total.text}</td><td className="amount">¥{total.cost.toFixed(2)}</td></tr>
          </tbody>
        </table></div>
      )}
    </section>
  )
}
