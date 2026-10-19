import { useEffect, useState, type ReactNode } from 'react'
import { Save } from 'lucide-react'
import { api } from '../lib/api'
import { useToast, errText } from '../lib/toast'
import { Topbar, Loading, Field, Tabs } from '../components/ui'
import { CategoriesTab } from './Categories'
import { TaskRulesTab } from './TaskRules'

type Settings = { ai_models?: Record<string, string>; ai_prices?: Record<string, number>; quota_default?: { month_images: number | null; month_text: number | null } }

const FEATURES: Record<string, string> = {
  classify: '照片分拣（看图）',
  tag: '图片打标签（看图）',
  copy: '写文案',
  copy_rewrite: '改写文案',
  copy_check: '文案合规审查',
  enhance: '一键出图·高清化',
  fill: 'AI 补图',
  upscale: '图片处理·高清化',
  process: '图片处理·AI 改图',
  model_create: '模特库·创建与参考图',
  tryon_analyze: '试穿·商品识别（看图）',
  tryon: '试穿·生成穿戴照',
  assistant: 'AI 助手',
}

export function SettingsPage() {
  const [tab, setTab] = useState<'general' | 'categories' | 'rules'>('general')
  const tabs = <Tabs value={tab} onChange={setTab} items={[{ key: 'general', label: '通用' }, { key: 'categories', label: '品类' }, { key: 'rules', label: '自动派活' }]} />
  if (tab === 'categories') return (
    <>
      <Topbar title="设置" sub="品类改完立即生效，新的分拣、文案和出图按新设置来" />
      <div className="scroll"><main className="content" style={{ maxWidth: 1080 }}>{tabs}<CategoriesTab /></main></div>
    </>
  )
  if (tab === 'rules') return (
    <>
      <Topbar title="设置" sub="规则改完立即生效，只对之后发生的事件派活" />
      <div className="scroll"><main className="content" style={{ maxWidth: 1080 }}>{tabs}<TaskRulesTab /></main></div>
    </>
  )
  return <GeneralSettings tabs={tabs} />
}

function GeneralSettings({ tabs }: { tabs: ReactNode }) {
  const toast = useToast()
  const [s, setS] = useState<Settings | null>(null)
  const [models, setModels] = useState<Record<string, string>>({})
  const [prices, setPrices] = useState<Record<string, string>>({})
  const [quota, setQuota] = useState({ month_images: '', month_text: '' })
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    api<Settings>('/api/settings').then(r => {
      setS(r)
      setModels(r.ai_models || {})
      setPrices(Object.fromEntries(Object.entries(r.ai_prices || {}).map(([k, v]) => [k, String(v)])))
      setQuota({ month_images: r.quota_default?.month_images?.toString() ?? '', month_text: r.quota_default?.month_text?.toString() ?? '' })
    }).catch(e => toast(errText(e), 'error'))
  }, [toast])
  if (!s) return <><Topbar crumbs={['设置']} /><Loading /></>
  const modelNames = [...new Set(Object.values(models).filter(Boolean))]
  const num = (v: string) => (v.trim() === '' ? null : Math.max(0, Math.floor(Number(v))))
  const save = async () => {
    setBusy(true)
    try {
      await api('/api/settings', { method: 'PATCH', body: {
        ai_models: models,
        ai_prices: Object.fromEntries(Object.entries(prices).filter(([, v]) => v.trim() !== '').map(([k, v]) => [k, Number(v)])),
        quota_default: { month_images: num(quota.month_images), month_text: num(quota.month_text) }
      } })
      toast('设置已保存', 'ok')
    } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  return (
    <>
      <Topbar title="设置" sub="改完点右上角保存，新的出图任务才会用到新设置"><button className="btn btn-primary" disabled={busy} onClick={save}><Save />保存设置</button></Topbar>
      <div className="scroll"><main className="content" style={{ maxWidth: 880 }}>
        {tabs}
        <section className="panel">
          <div className="panel-head"><div className="titles"><div className="panel-title">AI 模型</div><div className="panel-sub">每个功能用哪个模型；留空用服务器环境变量里的默认值</div></div></div>
          <div className="panel-pad stack" style={{ gap: 10 }}>
            {Object.entries(FEATURES).map(([k, t]) => (
              <div key={k} className="setting"><div className="cell-title">{t}</div>
                <input className="input mono" style={{ width: 260 }} value={models[k] || ''} onChange={e => setModels({ ...models, [k]: e.target.value })} /></div>
            ))}
          </div>
        </section>

        <section className="panel">
          <div className="panel-head"><div className="titles"><div className="panel-title">模型单价</div><div className="panel-sub">只用来估算费用（元 / 次，生图元 / 张），不影响调用</div></div></div>
          <div className="panel-pad stack" style={{ gap: 10 }}>
            {modelNames.length === 0 && <div className="t3">先在上面填模型名。</div>}
            {modelNames.map(m => (
              <div key={m} className="setting"><div className="cell-title mono">{m}</div>
                <input className="input mono" style={{ width: 140 }} inputMode="decimal" placeholder="0.00" value={prices[m] ?? ''} onChange={e => setPrices({ ...prices, [m]: e.target.value })} /></div>
            ))}
          </div>
        </section>

        <section className="panel">
          <div className="panel-head"><div className="titles"><div className="panel-title">新账号默认额度</div><div className="panel-sub">每月；留空表示不限。已有账号的额度在账号页单独改</div></div></div>
          <div className="panel-pad form-row">
            <Field label="AI 生图（张 / 月）"><input className="input mono" inputMode="numeric" value={quota.month_images} onChange={e => setQuota({ ...quota, month_images: e.target.value })} /></Field>
            <Field label="文本 AI（次 / 月）"><input className="input mono" inputMode="numeric" value={quota.month_text} onChange={e => setQuota({ ...quota, month_text: e.target.value })} /></Field>
          </div>
        </section>
      </main></div>
    </>
  )
}
