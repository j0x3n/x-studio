import { useEffect, useState } from 'react'
import { Save } from 'lucide-react'
import { api } from '../lib/api'
import { useToast, errText } from '../lib/toast'
import { Topbar, PageHead, Loading, Field } from '../components/ui'

type Label = { text: string; position: string; size: number; color: string; opacity: number }
type Settings = { ai_label?: Partial<Label>; ai_models?: Record<string, string>; ai_prices?: Record<string, number>; quota_default?: { month_images: number | null; month_text: number | null } }

const POS: Record<string, string> = { 'top-left': '左上', 'top-right': '右上', 'bottom-left': '左下', 'bottom-right': '右下' }
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
}

export function SettingsPage() {
  const toast = useToast()
  const [s, setS] = useState<Settings | null>(null)
  const [label, setLabel] = useState<Label>({ text: 'AI 生成', position: 'bottom-right', size: 14, color: '#FFFFFF', opacity: 0.6 })
  const [models, setModels] = useState<Record<string, string>>({})
  const [prices, setPrices] = useState<Record<string, string>>({})
  const [quota, setQuota] = useState({ month_images: '', month_text: '' })
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    api<Settings>('/api/settings').then(r => {
      setS(r)
      setLabel(l => ({ ...l, ...(r.ai_label || {}) }))
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
        ai_label: { ...label, size: Number(label.size), opacity: Number(label.opacity) },
        ai_models: models,
        ai_prices: Object.fromEntries(Object.entries(prices).filter(([, v]) => v.trim() !== '').map(([k, v]) => [k, Number(v)])),
        quota_default: { month_images: num(quota.month_images), month_text: num(quota.month_text) }
      } })
      toast('设置已保存', 'ok')
    } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const [v, h] = label.position.split('-')
  return (
    <>
      <Topbar crumbs={['设置']}><button className="btn btn-primary" disabled={busy} onClick={save}><Save />保存设置</button></Topbar>
      <div className="scroll"><main className="content" style={{ maxWidth: 880 }}>
        <PageHead title="设置" sub="改完点右上角保存。新的出图任务才会用到新设置。" />

        <section className="panel">
          <div className="panel-head"><div className="titles"><div className="panel-title">AI 标识</div><div className="panel-sub">AI 改图和 AI 生成的图，导出时默认打上这个角标，并在文件里写入隐式标识</div></div></div>
          <div className="panel-pad row" style={{ gap: 24, alignItems: 'flex-start', flexWrap: 'wrap' }}>
            <div className="stack grow" style={{ gap: 12, minWidth: 280 }}>
              <div className="form-row">
                <Field label="文字"><input className="input" maxLength={50} value={label.text} onChange={e => setLabel({ ...label, text: e.target.value })} /></Field>
                <Field label="位置"><select className="input" value={label.position} onChange={e => setLabel({ ...label, position: e.target.value })}>
                  {Object.entries(POS).map(([k, t]) => <option key={k} value={k}>{t}</option>)}</select></Field>
              </div>
              <div className="form-row">
                <Field label="字号（px，按 750 宽的图）"><input className="input mono" type="number" min={10} max={48} value={label.size} onChange={e => setLabel({ ...label, size: Number(e.target.value) })} /></Field>
                <Field label="文字颜色"><input className="input mono" value={label.color} onChange={e => setLabel({ ...label, color: e.target.value })} /></Field>
              </div>
              <Field label={`底色不透明度 ${Math.round(label.opacity * 100)}%`}>
                <input type="range" min={0} max={1} step={0.05} value={label.opacity} onChange={e => setLabel({ ...label, opacity: Number(e.target.value) })} />
              </Field>
              <div className="fs-12 t3">导出时能不能取消这个标识，在「账号与权限 → 角色和权限」里由「导出时取消 AI 标识」控制，每次取消都会记日志。</div>
            </div>
            <div className="label-preview">
              <span style={{ [v]: 10, [h]: 10, fontSize: label.size * 0.32 + 'px', color: label.color, background: `rgba(0,0,0,${0.6 * label.opacity})` } as React.CSSProperties}>{label.text || 'AI 生成'}</span>
            </div>
          </div>
        </section>

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
