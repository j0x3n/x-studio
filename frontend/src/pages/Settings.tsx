import { useEffect, useState, type ReactNode } from 'react'
import { Save } from 'lucide-react'
import { api } from '../lib/api'
import { useToast, errText } from '../lib/toast'
import { Topbar, Loading, Field, Tabs } from '../components/ui'
import { CategoriesTab } from './Categories'
import { TaskRulesTab } from './TaskRules'

type Settings = { ai_models?: Record<string, string>; ai_prices?: Record<string, number>; quota_default?: { month_images: number | null; month_text: number | null } }
type ItemCodeRule = { prefix: string; date: 'none' | 'day' | 'month' | 'year'; separator: string; digits: number; start: number }
const DEFAULT_CODE_RULE: ItemCodeRule = { prefix: 'WL', date: 'none', separator: '', digits: 6, start: 1 }

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
  document_analyze: '采购单据识别（看图）',
  finance_explain: '财务·解释对账差异',
  creative: 'AI 出图·生成和改图',
  creative_verify: 'AI 出图·复核（看图）',
  creative_text: 'AI 出图·图上文字核对（看图）',
  title_suggest: '标题优化',
  ops_advice: '运营诊断解释',
}

export function SettingsPage() {
  const [tab, setTab] = useState<'general' | 'codes' | 'categories' | 'rules'>('general')
  const tabs = <Tabs value={tab} onChange={setTab} items={[{ key: 'general', label: '通用' }, { key: 'codes', label: '编码' }, { key: 'categories', label: '品类' }, { key: 'rules', label: '自动派活' }]} />
  if (tab === 'codes') return <CodeSettings tabs={tabs} />
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

function CodeSettings({ tabs }: { tabs: ReactNode }) {
  const toast = useToast()
  const [rule, setRule] = useState<ItemCodeRule>(DEFAULT_CODE_RULE)
  const [digits, setDigits] = useState('6')
  const [start, setStart] = useState('1')
  const [loaded, setLoaded] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const load = async () => {
    setError('')
    try {
      const settings = await api<{ item_code_rule?: ItemCodeRule }>('/api/settings')
      const saved = settings.item_code_rule || DEFAULT_CODE_RULE
      setRule(saved)
      setDigits(String(saved.digits))
      setStart(String(saved.start))
      setLoaded(true)
    } catch (e) { setError(errText(e)) }
  }
  useEffect(() => { void load() }, [])
  const valid = /^[A-Za-z0-9_-]{0,16}$/.test(rule.prefix) && /^\d+$/.test(digits) && Number(digits) >= 1 && Number(digits) <= 12 && /^\d+$/.test(start) && Number(start) >= 1 && Number(start) <= 999999999999
  const today = new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(new Date())
  const date = ['year', 'month', 'day'].map(k => today.find(p => p.type === k)?.value || '').join('')
  const datePart = rule.date === 'day' ? date : rule.date === 'month' ? date.slice(0, 6) : rule.date === 'year' ? date.slice(0, 4) : ''
  const preview = valid ? [rule.prefix, datePart, String(Number(start)).padStart(Number(digits), '0')].filter(Boolean).join(rule.separator) : '请填写有效规则'
  const save = async () => {
    if (!valid || !loaded || busy) return
    setBusy(true)
    try {
      await api('/api/settings', { method: 'PATCH', body: { item_code_rule: { ...rule, digits: Number(digits), start: Number(start) } } })
      toast('编码规则已保存', 'ok')
    } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  return <>
    <Topbar title="设置" sub="编码规则保存后，对新建物料的自动编码生效"><button className="btn btn-primary" disabled={!loaded || !valid || busy} onClick={save}><Save />保存规则</button></Topbar>
    <div className="scroll"><main className="content" style={{ maxWidth: 880 }}>
      {tabs}
      {error ? <div className="panel panel-pad stack"><span className="t3">{error}</span><div><button className="btn btn-secondary" onClick={load}>重新加载</button></div></div> : !loaded ? <Loading /> : <section className="panel">
        <div className="panel-head"><div className="titles"><div className="panel-title">物料编码</div><div className="panel-sub">成品、原料配件、包装耗材共用；新建时留空自动生成，也可手动填写</div></div></div>
        <div className="panel-pad stack">
          <div className="form-row">
            <Field label="前缀" hint="最多16位，支持英文字母、数字、横线和下划线，可留空"><input className="input mono" maxLength={16} placeholder="如 WL、SKU、MAT" value={rule.prefix} onChange={e => setRule({ ...rule, prefix: e.target.value })} /></Field>
            <Field label="日期" hint="按北京时间；选用日期后，每个日期单独计数"><select className="input" value={rule.date} onChange={e => setRule({ ...rule, date: e.target.value as ItemCodeRule['date'] })}><option value="none">不包含日期</option><option value="day">年月日（YYYYMMDD）</option><option value="month">年月（YYYYMM）</option><option value="year">年份（YYYY）</option></select></Field>
          </div>
          <div className="form-row">
            <Field label="分隔符"><select className="input" value={rule.separator} onChange={e => setRule({ ...rule, separator: e.target.value })}><option value="">无</option><option value="-">横线 -</option><option value="_">下划线 _</option></select></Field>
            <Field label="序号位数" hint="1–12位，不足补0；超出位数后正常递增"><input className="input mono" type="number" min={1} max={12} step={1} value={digits} onChange={e => setDigits(e.target.value)} /></Field>
          </div>
          <div className="form-row">
            <Field label="起始序号" hint="首次使用从此序号起；已使用的规则继续递增，调小不会回退"><input className="input mono" type="number" min={1} max={999999999999} step={1} value={start} onChange={e => setStart(e.target.value)} /></Field>
            <Field label="样式预览" hint="按起始序号展示，实际编码会继续递增并跳过已占用编码"><div className="input mono" style={{ display: 'flex', alignItems: 'center' }}>{preview}</div></Field>
          </div>
          <div className="t3">修改规则不改变已有物料编码；切回已用过的前缀和日期时，接着原序号生成。</div>
        </div>
      </section>}
    </main></div>
  </>
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
