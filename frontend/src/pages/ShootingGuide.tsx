import { useMemo, useState } from 'react'
import { Copy } from 'lucide-react'
import { useCategories } from '../lib/categories'
import { useToast } from '../lib/toast'
import type { Category, ShotCode } from '../lib/types'
import { Topbar, Pill, Loading, Empty, Tabs } from '../components/ui'
import { KnowledgeTabs } from '../components/KnowledgeTabs'

type Slot = { 位?: number | string; 屏?: number | string; 名: string; 候选: string[]; 必需?: boolean; 最少?: number; 备注?: string }

const TIPS = [
  '手机就够。开微距，关美颜、关滤镜、关闪光灯。',
  '窗边自然光，阴天也行。不要阳光直射，会拍成死白或者偏色。',
  '每张拍 2–3 次，挑最清楚的发过来。发原图（微信选「原图」），不要截图、不要加水印。',
  '必须是这一批要发的货，不要用以前的图或别家的图。'
]
const WHY = '淘宝严查和实物不符的 AI 图，买家收到货不一样会退货，还会被处罚。所以商品本身必须实拍，系统只做抠白底、轻微调亮、裁剪和排版，不改颜色和光泽。缺了必拍的照片会提示补拍，不会用 AI 补。'

const slotsOf = (c: Category) => {
  const s = c.slots as { 主图?: Slot[]; 详情页?: Slot[] }
  return { main: s.主图 || [], detail: s.详情页 || [] }
}
const label = (c: ShotCode) => (c.suffix ? `${c.code}_<${c.suffix}>` : c.code)

// 必拍：每个必需位置的第一候选
function mustShoot(c: Category) {
  const { main, detail } = slotsOf(c)
  const out: { code: ShotCode; for: string[] }[] = []
  for (const [kind, list] of [['主图', main], ['详情页', detail]] as const) {
    for (const s of list) {
      if (!s.必需 || !s.候选?.length) continue
      const code = c.codes.find(x => x.code === s.候选[0])
      if (!code) continue
      const where = `${kind}${s.位 !== undefined ? `第 ${s.位} 张` : s.屏 !== undefined ? `第 ${s.屏} 屏` : ''}·${s.名}`.replace('第 白底 张', '白底')
      const hit = out.find(o => o.code.code === code.code)
      if (hit) hit.for.push(where)
      else out.push({ code, for: [where] })
    }
  }
  return out
}

export function ShootingGuide() {
  const toast = useToast()
  const { items: cats } = useCategories()
  const [key, setKey] = useState('')
  const cat = cats?.find(c => c.key === key) || cats?.[0]
  const must = useMemo(() => (cat ? mustShoot(cat) : []), [cat])
  const groups = useMemo(() => {
    const m = new Map<string, ShotCode[]>()
    for (const c of cat?.codes || []) m.set(c.group, [...(m.get(c.group) || []), c])
    return [...m.entries()]
  }, [cat])

  // 发给上游用的纯文字版
  const copy = async () => {
    if (!cat) return
    const text = [
      `【${cat.name}拍摄要求】`,
      '', '必拍：',
      ...must.map(m => `${label(m.code)} ${m.code.name}：${m.code.desc || ''}`),
      '', '怎么拍：', ...TIPS.map((t, i) => `${i + 1}. ${t}`),
      '', '照片文件名用编号最方便，比如 B1.jpg、C4_8mm.jpg；不改名也行，系统会自动分。'
    ].join('\n')
    try { await navigator.clipboard.writeText(text); toast('已复制，可以直接发微信', 'ok') } catch { toast('复制失败，手动选中文字复制', 'error') }
  }

  if (!cats) return <><Topbar crumbs={['知识库', '拍摄要求']} /><div className="scroll"><main className="content"><KnowledgeTabs /><Loading /></main></div></>
  if (!cat) return <><Topbar crumbs={['知识库', '拍摄要求']} /><div className="scroll"><main className="content"><KnowledgeTabs /><Empty title="还没有品类" desc="先在「设置 → 品类」里建一个。" /></main></div></>
  const { main, detail } = slotsOf(cat)
  return (
    <>
      <Topbar crumbs={['知识库']} title="拍摄要求" sub="每个品类拍哪几张、怎么拍。必拍的缺了，出图时会提醒补拍。">
        <button className="btn btn-secondary" onClick={copy}><Copy />复制发给上游</button>
      </Topbar>
      <div className="scroll"><main className="content" style={{ maxWidth: 1080 }}>
        <KnowledgeTabs />
        {cats.length > 1 && <Tabs value={cat.key} onChange={setKey} items={cats.map(c => ({ key: c.key, label: c.name }))} />}
        <div className="two-col">
          <section className="panel">
            <div className="panel-head"><div className="titles"><div className="panel-title">必拍</div><div className="panel-sub">拍齐这 {must.length} 张就能出全套主图和详情页</div></div></div>
            <div className="table-wrap"><table className="table">
              <thead><tr><th style={{ width: 88 }}>编号</th><th>拍什么</th><th>用在</th></tr></thead>
              <tbody>{must.map(m => (
                <tr key={m.code.code}>
                  <td className="mono">{label(m.code)}</td>
                  <td><div className="cell-title">{m.code.name}</div><div className="cell-sub">{m.code.desc}</div></td>
                  <td className="fs-12 t2">{m.for.join('、')}</td>
                </tr>
              ))}</tbody>
            </table></div>
          </section>
          <section className="panel">
            <div className="panel-head"><div className="panel-title">怎么拍</div></div>
            <div className="panel-pad stack">
              <ol className="guide-tips">{TIPS.map(t => <li key={t}>{t}</li>)}</ol>
              <div className="fs-12 t3">{WHY}</div>
            </div>
          </section>
        </div>

        <section className="panel">
          <div className="panel-head"><div className="titles"><div className="panel-title">全部拍摄编号</div><div className="panel-sub">分拣时 AI 按这张表给照片定编号；带 _&lt;…&gt; 的可以按规格或尺寸拍多张</div></div></div>
          <div className="table-wrap"><table className="table">
            <thead><tr><th style={{ width: 96 }}>分组</th><th style={{ width: 88 }}>编号</th><th>拍什么</th><th>用在哪</th></tr></thead>
            <tbody>{groups.map(([g, list]) => list.map((c, i) => (
              <tr key={c.code}>
                {i === 0 && <td rowSpan={list.length} className="t2" style={{ verticalAlign: 'top' }}>{g}</td>}
                <td className="mono">{label(c)}{must.some(m => m.code.code === c.code) && <> <Pill kind="pill-warning" dot={false}>必拍</Pill></>}</td>
                <td><div className="cell-title">{c.name}</div>{c.desc && <div className="cell-sub">{c.desc}</div>}</td>
                <td className="fs-12 t2">{c.use || '—'}</td>
              </tr>
            )))}</tbody>
          </table></div>
        </section>

        <div className="two-col">
          {([['主图', main, '位'], ['详情页', detail, '屏']] as const).map(([title, list, k]) => (
            <section key={title} className="panel">
              <div className="panel-head"><div className="titles"><div className="panel-title">{title}取图顺序</div><div className="panel-sub">每个位置按顺序取第一张有的照片</div></div></div>
              <div className="table-wrap"><table className="table">
                <thead><tr><th style={{ width: 56 }}>{k}</th><th>位置</th><th>候选编号</th></tr></thead>
                <tbody>{list.map((s, i) => (
                  <tr key={i}>
                    <td className="mono">{String(s[k] ?? '')}</td>
                    <td><span className="cell-title">{s.名}</span>{s.必需 && <> <Pill kind="pill-warning" dot={false}>必需</Pill></>}{s.备注 && <div className="cell-sub">{s.备注}</div>}</td>
                    <td className="mono fs-12">{s.候选.join(' › ')}</td>
                  </tr>
                ))}</tbody>
              </table></div>
            </section>
          ))}
        </div>
      </main></div>
    </>
  )
}
