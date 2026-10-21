import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Search, Upload, KeyRound, Copy, Trash2, Plus, Save, FileSpreadsheet, Check } from 'lucide-react'
import { useAuth } from '../lib/auth'
import { useToast, errText } from '../lib/toast'
import { useCategories } from '../lib/categories'
import { fmtAgo, fmtDate } from '../lib/format'
import { Topbar, Pill, Loading, Empty, Tabs, Field, Drawer } from '../components/ui'
import { IMPORT_FIELDS, KIND, MATERIALS, bps, guessMap, mapRows, marketApi, n, readCsv, yuan, type AdRow, type Competitor, type IngestResult, type Keyword, type Preview, type Snapshot, type Token } from '../lib/market'
import '../styles/market.css'

type TabKey = 'keywords' | 'competitors' | 'ads' | 'import' | 'collect'

// 数据页：关键词、竞品、推广数据，报表导入，采集助手的令牌和采集记录
export function Market() {
  const { me, can } = useAuth()
  const shops = me?.shops || []
  const [shopId, setShopId] = useState(shops[0]?.id || 0)
  const [tab, setTab] = useState<TabKey>('keywords')
  const imp = can('market.import')
  if (!shopId) return <><Topbar title="数据" /><div className="scroll"><main className="content"><Empty title="没有可看的店铺" /></main></div></>
  return (
    <>
      <Topbar title="数据" sub="关键词、竞品和推广数据；用采集助手一键发送，或导入生意参谋、万相台的报表">
        {shops.length > 1 && <select className="input" style={{ width: 160 }} value={shopId} onChange={e => setShopId(Number(e.target.value))}>{shops.map(s => <option key={s.id} value={s.id}>{s.name}</option>)}</select>}
      </Topbar>
      <div className="scroll"><main className="content">
        <Tabs value={tab} onChange={setTab} items={[
          { key: 'keywords', label: '关键词' }, { key: 'competitors', label: '竞品' }, { key: 'ads', label: '推广' },
          ...(imp ? [{ key: 'import' as const, label: '导入报表' }, { key: 'collect' as const, label: '采集助手' }] : [])
        ]} />
        {tab === 'keywords' && <Keywords key={shopId} shopId={shopId} />}
        {tab === 'competitors' && <Competitors key={shopId} shopId={shopId} />}
        {tab === 'ads' && <Ads key={shopId} shopId={shopId} />}
        {tab === 'import' && <Import key={shopId} shopId={shopId} />}
        {tab === 'collect' && <Collect key={shopId} shopId={shopId} />}
      </main></div>
    </>
  )
}

function Spark({ pts }: { pts: (number | null)[] }) {
  const v = pts.filter((x): x is number => typeof x === 'number')
  if (v.length < 2) return <span className="t4">—</span>
  const max = Math.max(...v), min = Math.min(...v), r = max - min || 1
  const d = pts.map((p, i) => (typeof p === 'number' ? `${(i / (pts.length - 1)) * 60},${18 - ((p - min) / r) * 16}` : null)).filter(Boolean).join(' ')
  return <svg className="mk-spark" viewBox="0 0 60 20" aria-hidden><polyline points={d} /></svg>
}

function Keywords({ shopId }: { shopId: number }) {
  const toast = useToast()
  const { items: cats } = useCategories()
  const [cat, setCat] = useState('')
  const [q, setQ] = useState('')
  const [sort, setSort] = useState('score')
  const [days, setDays] = useState(30)
  const [rows, setRows] = useState<Keyword[] | null>(null)
  const load = useCallback(() => marketApi.keywords({ shop_id: shopId, category: cat || undefined, q: q.trim() || undefined, days, sort }).then(r => setRows(r.items || [])).catch(e => { toast(errText(e), 'error'); setRows([]) }), [shopId, cat, q, days, sort, toast])
  useEffect(() => { const t = setTimeout(load, 250); return () => clearTimeout(t) }, [load])
  return (
    <section className="panel">
      <div className="panel-head">
        <div className="titles"><div className="panel-title">关键词</div><div className="panel-sub">价值 = 搜索量 × 转化率；缺一项的排在最后。数据来自万相台关键词工具</div></div>
        <label className="field" style={{ width: 200 }}><Search /><input placeholder="搜关键词" value={q} onChange={e => setQ(e.target.value)} /></label>
        {(cats?.length || 0) > 1 && <select className="input" style={{ width: 110 }} value={cat} onChange={e => setCat(e.target.value)}><option value="">全部品类</option>{cats!.map(c => <option key={c.id} value={c.key}>{c.name}</option>)}</select>}
        <div className="segmented">{[7, 30, 90].map(d => <button key={d} className={days === d ? 'active' : ''} onClick={() => setDays(d)}>{d} 天</button>)}</div>
        <select className="input" style={{ width: 110 }} value={sort} onChange={e => setSort(e.target.value)}><option value="score">按价值</option><option value="volume">按搜索量</option><option value="cvr">按转化率</option></select>
      </div>
      {!rows ? <Loading /> : !rows.length ? <Empty title="还没有关键词数据" desc="在万相台关键词工具页面，用采集助手点「发送到 X Studio」。" /> : (
        <div className="table-wrap"><table className="table">
          <thead><tr><th>关键词</th><th className="num">价值</th><th className="num">搜索量</th><th>近 7 天</th><th className="num">点击</th><th className="num">点击率</th><th className="num">转化率</th><th className="num">竞争度</th><th>日期</th></tr></thead>
          <tbody>{rows.map(k => (
            <tr key={k.word + k.date}>
              <td className="cell-title">{k.word}</td>
              <td className="num mono">{k.score === null ? <span className="t4">缺数据</span> : n(Math.round(k.score))}</td>
              <td className="num mono">{n(k.search_volume)}</td>
              <td><Spark pts={k.trend.map(t => t.search_volume)} /></td>
              <td className="num mono">{n(k.clicks)}</td><td className="num mono">{bps(k.ctr_bps)}</td><td className="num mono">{bps(k.cvr_bps)}</td><td className="num mono">{n(k.competition)}</td>
              <td className="t3 mono">{k.date}</td>
            </tr>
          ))}</tbody>
        </table></div>
      )}
    </section>
  )
}

function Competitors({ shopId }: { shopId: number }) {
  const toast = useToast()
  const [kw, setKw] = useState('')
  const [mat, setMat] = useState('')
  const [rows, setRows] = useState<Competitor[] | null>(null)
  const load = useCallback(() => marketApi.competitors({ shop_id: shopId, keyword: kw.trim() || undefined, material: mat || undefined }).then(r => setRows(r.items || [])).catch(e => { toast(errText(e), 'error'); setRows([]) }), [shopId, kw, mat, toast])
  useEffect(() => { const t = setTimeout(load, 250); return () => clearTimeout(t) }, [load])
  return (
    <section className="panel">
      <div className="panel-head">
        <div className="titles"><div className="panel-title">竞品</div><div className="panel-sub">淘宝搜索结果和商品页采到的；材质只按标题和参数里写的字判断，不按价格推</div></div>
        <label className="field" style={{ width: 200 }}><Search /><input placeholder="按搜索词筛" value={kw} onChange={e => setKw(e.target.value)} /></label>
        <select className="input" style={{ width: 110 }} value={mat} onChange={e => setMat(e.target.value)}><option value="">全部材质</option>{MATERIALS.map(m => <option key={m}>{m}</option>)}</select>
      </div>
      {!rows ? <Loading /> : !rows.length ? <Empty title="还没有竞品数据" desc="在淘宝搜索结果页用采集助手发送。" /> : (
        <div className="table-wrap"><table className="table">
          <thead><tr><th className="num">排名</th><th style={{ width: 56 }} /><th>标题</th><th>材质</th><th className="num">价格</th><th>销量</th><th>店铺</th><th>搜索词</th></tr></thead>
          <tbody>{rows.map(c => (
            <tr key={c.platform_item_id + c.keyword + c.date}>
              <td className="num mono">{c.rank ?? '—'}</td>
              <td>{c.image_url ? <img className="mk-thumb" src={c.image_url} alt="" loading="lazy" referrerPolicy="no-referrer" /> : <span className="mk-thumb" />}</td>
              <td><div className="ellipsis" style={{ maxWidth: 360 }} title={c.title}>{c.title}</div><div className="cell-sub mono">{c.platform_item_id}</div></td>
              <td>{c.material === '未写' ? <span className="t4">未写</span> : <span className={'tag ' + (c.material === '淡水' || c.material === '天然' ? 'tag-green' : 'tag-orange')}>{c.material}</span>}</td>
              <td className="num mono">{yuan(c.price_cents)}</td>
              <td className="t2">{c.sales_text || '—'}</td><td className="t2">{c.shop_name}</td><td className="t3">{c.keyword}</td>
            </tr>
          ))}</tbody>
        </table></div>
      )}
    </section>
  )
}

function Ads({ shopId }: { shopId: number }) {
  const toast = useToast()
  const [from, setFrom] = useState(() => new Date(Date.now() + 8 * 3600_000 - 7 * 86400_000).toISOString().slice(0, 10))
  const [to, setTo] = useState(() => new Date(Date.now() + 8 * 3600_000).toISOString().slice(0, 10))
  const [rows, setRows] = useState<AdRow[] | null>(null)
  useEffect(() => { marketApi.ads({ shop_id: shopId, from, to }).then(r => setRows(r.items || [])).catch(e => { toast(errText(e), 'error'); setRows([]) }) }, [shopId, from, to, toast])
  const sum = useMemo(() => (rows || []).reduce((a, r) => ({ cost: a.cost + (r.cost_cents || 0), gmv: a.gmv + (r.gmv_cents || 0), clicks: a.clicks + (r.clicks || 0), orders: a.orders + (r.orders || 0) }), { cost: 0, gmv: 0, clicks: 0, orders: 0 }), [rows])
  return (
    <section className="panel">
      <div className="panel-head">
        <div className="titles"><div className="panel-title">推广</div><div className="panel-sub">万相台报表导入的每日数据；广告操作在万相台自己做，这里只看</div></div>
        <input type="date" className="input" style={{ width: 140 }} value={from} onChange={e => setFrom(e.target.value)} /><span className="t3">至</span><input type="date" className="input" style={{ width: 140 }} value={to} onChange={e => setTo(e.target.value)} />
      </div>
      {rows && rows.length > 0 && (
        <div className="stat-strip">
          <div><div className="k">花费</div><div className="v">{yuan(sum.cost)}</div></div>
          <div><div className="k">成交额</div><div className="v">{yuan(sum.gmv)}</div></div>
          <div><div className="k">ROI</div><div className="v">{sum.cost ? (sum.gmv / sum.cost).toFixed(2) : '—'}</div></div>
          <div><div className="k">点击 / 成交</div><div className="v">{n(sum.clicks)} / {n(sum.orders)}</div></div>
        </div>
      )}
      {!rows ? <Loading /> : !rows.length ? <Empty title="这段时间没有推广数据" desc="在「导入报表」里导入万相台的报表。" /> : (
        <div className="table-wrap"><table className="table">
          <thead><tr><th>日期</th><th>计划</th><th>关键词</th><th className="num">花费</th><th className="num">展现</th><th className="num">点击</th><th className="num">成交</th><th className="num">成交额</th><th className="num">ROI</th></tr></thead>
          <tbody>{rows.map((r, i) => (
            <tr key={i}>
              <td className="mono t3">{r.date}</td><td>{r.campaign || '—'}</td><td className="cell-title">{r.keyword || '—'}</td>
              <td className="num mono">{yuan(r.cost_cents)}</td><td className="num mono">{n(r.impressions)}</td><td className="num mono">{n(r.clicks)}</td><td className="num mono">{n(r.orders)}</td><td className="num mono">{yuan(r.gmv_cents)}</td>
              <td className="num mono">{r.cost_cents && r.gmv_cents !== null ? (r.gmv_cents / r.cost_cents).toFixed(2) : '—'}</td>
            </tr>
          ))}</tbody>
        </table></div>
      )}
    </section>
  )
}

function Import({ shopId }: { shopId: number }) {
  const toast = useToast()
  const input = useRef<HTMLInputElement>(null)
  const [kind, setKind] = useState<'my_listing' | 'ad'>('my_listing')
  const [file, setFile] = useState('')
  const [grid, setGrid] = useState<string[][] | null>(null)
  const [headRow, setHeadRow] = useState(0)
  const [map, setMap] = useState<Record<string, number>>({})
  const [pv, setPv] = useState<Preview | null>(null)
  const [done, setDone] = useState<IngestResult | null>(null)
  const [busy, setBusy] = useState(false)
  const header = grid?.[headRow] || []
  const body = (grid || []).slice(headRow + 1)
  const fields = IMPORT_FIELDS[kind]
  // 报表前几行常是说明文字：找第一行能猜中两个以上字段的当表头
  const open = async (f: File) => {
    try {
      const g = await readCsv(f)
      if (!g.length) return toast('文件是空的', 'error')
      const h = Math.max(0, g.slice(0, 10).findIndex(r => Object.values(guessMap(kind, r)).filter(i => i >= 0).length >= 2))
      setFile(f.name); setGrid(g); setHeadRow(h); setMap(guessMap(kind, g[h])); setPv(null); setDone(null)
    } catch (e) { toast(errText(e), 'error') }
  }
  useEffect(() => { if (grid) setMap(guessMap(kind, grid[headRow] || [])); setPv(null) }, [kind, headRow]) // eslint-disable-line react-hooks/exhaustive-deps
  const preview = async () => {
    const miss = fields.filter(f => f.required && !(map[f.key] >= 0))
    if (miss.length) return toast(`先对应好：${miss.map(f => f.label).join('、')}`, 'error')
    if (body.length > 5000) return toast('一次最多 5000 行，拆开导', 'error')
    setBusy(true)
    try { setPv(await marketApi.preview({ shop_id: shopId, kind, rows: mapRows(kind, body, map) })) } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const confirm = async () => {
    if (!pv) return
    setBusy(true)
    try { const r = await marketApi.confirm(pv.preview_id); setDone(r); setPv(null); toast(r.duplicate ? '这份数据导过了' : `已导入：新增 ${r.inserted}，更新 ${r.updated}`, 'ok') } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  return (
    <section className="panel">
      <div className="panel-head">
        <div className="titles"><div className="panel-title">导入报表</div><div className="panel-sub">CSV 在浏览器里解码（GBK 也行），先预览再确认；同一天同一条再导会覆盖，不会累加</div></div>
        <div className="segmented"><button className={kind === 'my_listing' ? 'active' : ''} onClick={() => setKind('my_listing')}>生意参谋 · 商品</button><button className={kind === 'ad' ? 'active' : ''} onClick={() => setKind('ad')}>万相台 · 推广</button></div>
      </div>
      <div className="panel-pad stack">
        <div className="drop" onClick={() => input.current?.click()} onDragOver={e => e.preventDefault()} onDrop={e => { e.preventDefault(); const f = e.dataTransfer.files[0]; if (f) open(f) }}>
          <FileSpreadsheet /><b>{file || '点击或拖入 CSV'}</b>{grid ? `${body.length} 行数据` : '从生意参谋或万相台导出的报表，存成 CSV'}
          <input ref={input} type="file" accept=".csv,text/csv,.txt" hidden onChange={e => { const f = e.target.files?.[0]; if (f) open(f); e.target.value = '' }} />
        </div>
        {grid && (
          <>
            <div className="row" style={{ gap: 8 }}><span className="fs-12 t3">表头在第</span><input className="input mono" style={{ width: 64 }} type="number" min={1} max={grid.length} value={headRow + 1} onChange={e => setHeadRow(Math.max(0, Math.min(grid.length - 1, Number(e.target.value) - 1)))} /><span className="fs-12 t3">行</span></div>
            <div className="mk-map">
              {fields.map(f => (
                <Field key={f.key} label={f.label} req={f.required}>
                  <select className="input" value={map[f.key] ?? -1} onChange={e => { setMap({ ...map, [f.key]: Number(e.target.value) }); setPv(null) }}>
                    <option value={-1}>（没有）</option>
                    {header.map((h, i) => <option key={i} value={i}>{h || `第 ${i + 1} 列`}</option>)}
                  </select>
                </Field>
              ))}
            </div>
            <div className="row" style={{ gap: 8 }}>
              <button className="btn btn-secondary" disabled={busy} onClick={preview}><Upload />预览</button>
              {pv && <button className="btn btn-primary" disabled={busy || !pv.ok} onClick={confirm}><Check />确认导入 {pv.ok} 行</button>}
            </div>
          </>
        )}
        {pv && (
          <div className="stack" style={{ gap: 8 }}>
            <div className="inline fs-13"><span>共 {pv.total} 行</span><Pill kind="pill-success">能导 {pv.ok}</Pill>{pv.errors.length > 0 && <Pill kind="pill-danger">有问题 {pv.errors.length}</Pill>}</div>
            {pv.errors.length > 0 && <div className="mk-errors">{pv.errors.slice(0, 50).map(e => <div key={e.index} className="fs-12"><span className="mono t3">第 {e.index + headRow + 2} 行</span> {e.reason}</div>)}</div>}
            <div className="table-wrap"><table className="table">
              <thead><tr>{fields.map(f => <th key={f.key}>{f.label}</th>)}</tr></thead>
              <tbody>{pv.sample.slice(0, 10).map((r, i) => <tr key={i}>{fields.map(f => <td key={f.key} className="mono fs-12">{r[f.key] === null || r[f.key] === undefined ? <span className="t4">—</span> : String(r[f.key])}</td>)}</tr>)}</tbody>
            </table></div>
          </div>
        )}
        {done && <div className="fs-13">已导入：新增 {done.inserted} 条，更新 {done.updated} 条{done.skipped.length ? `，跳过 ${done.skipped.length} 条` : ''}。运营建议会马上重算。</div>}
      </div>
    </section>
  )
}

function Collect({ shopId }: { shopId: number }) {
  const toast = useToast()
  const [tokens, setTokens] = useState<Token[] | null>(null)
  const [snaps, setSnaps] = useState<Snapshot[] | null>(null)
  const [budget, setBudget] = useState('')
  const [creating, setCreating] = useState(false)
  const [name, setName] = useState('')
  const [plain, setPlain] = useState('')
  const loadTokens = useCallback(() => marketApi.tokens().then(r => setTokens(r.items || [])).catch(e => { toast(errText(e), 'error'); setTokens([]) }), [toast])
  useEffect(() => { loadTokens() }, [loadTokens])
  useEffect(() => {
    marketApi.snapshots({ shop_id: shopId }).then(r => setSnaps(r.items || [])).catch(() => setSnaps([]))
    marketApi.shop(shopId).then(r => setBudget(r.daily_budget_cents === null ? '' : String(r.daily_budget_cents / 100))).catch(() => setBudget(''))
  }, [shopId])
  const create = async () => {
    if (!name.trim()) return toast('给令牌起个名字，比如「办公室 Chrome」', 'error')
    try { const r = await marketApi.createToken(name.trim()); setPlain(r.token); setName(''); loadTokens() } catch (e) { toast(errText(e), 'error') }
  }
  const revoke = async (t: Token) => {
    if (!confirm(`吊销「${t.name}」？用这个令牌的采集助手会马上失效。`)) return
    try { await marketApi.revokeToken(t.id); loadTokens() } catch (e) { toast(errText(e), 'error') }
  }
  const saveBudget = async () => {
    const v = budget.trim()
    if (v && !/^\d+(\.\d{1,2})?$/.test(v)) return toast('日预算写成 30 或 30.5 这样', 'error')
    try { await marketApi.saveShop(shopId, v ? Math.round(Number(v) * 100) : null); toast('日预算已保存', 'ok') } catch (e) { toast(errText(e), 'error') }
  }
  const active = (tokens || []).filter(t => !t.revoked_at)
  return (
    <div className="two-col top">
      <div className="stack">
        <section className="panel">
          <div className="panel-head">
            <div className="titles"><div className="panel-title">采集令牌</div><div className="panel-sub">采集助手用它把数据发回来，只能调「发送数据」这一个接口；每人最多 10 个</div></div>
            <button className="btn btn-secondary btn-sm" disabled={active.length >= 10} onClick={() => { setCreating(true); setPlain('') }}><Plus />新建令牌</button>
          </div>
          {!tokens ? <Loading /> : !tokens.length ? <Empty title="还没有令牌" desc="新建一个，填进浏览器里的采集助手。" /> : (
            <div className="list">{tokens.map(t => (
              <div key={t.id} className="list-item">
                <KeyRound className="t3" size={16} />
                <span className="grow stack" style={{ gap: 2, minWidth: 0 }}>
                  <span className="cell-title inline">{t.name}<span className="mono t4 fs-12">{t.prefix}…</span>{t.revoked_at && <span className="tag tag-gray">已吊销</span>}</span>
                  <span className="fs-12 t3">建于 {fmtDate(t.created_at)} · {t.last_used_at ? `最近使用 ${fmtAgo(t.last_used_at)}` : '还没用过'}</span>
                </span>
                {!t.revoked_at && <button className="btn btn-ghost btn-sm" onClick={() => revoke(t)}><Trash2 />吊销</button>}
              </div>
            ))}</div>
          )}
        </section>
        <section className="panel">
          <div className="panel-head"><div className="titles"><div className="panel-title">采集记录</div><div className="panel-sub">每次发送或导入记一条；同样的数据重复发会被认出来，不重复写</div></div></div>
          {!snaps ? <Loading /> : !snaps.length ? <Empty title="还没有采集过" /> : (
            <div className="table-wrap"><table className="table">
              <thead><tr><th>时间</th><th>类型</th><th>来源</th><th className="num">行数</th></tr></thead>
              <tbody>{snaps.map(s => <tr key={s.id}><td className="mono t3">{fmtDate(s.captured_at || s.created_at, true)}</td><td>{KIND[s.kind] || s.kind}</td><td>{s.source === 'script' ? '采集助手' : 'CSV 导入'}</td><td className="num mono">{s.rows}</td></tr>)}</tbody>
            </table></div>
          )}
        </section>
      </div>
      <div className="stack">
        <section className="panel">
          <div className="panel-head"><div className="panel-title">怎么用采集助手</div></div>
          <div className="panel-pad stack fs-13" style={{ gap: 6 }}>
            <div>1. Chrome 装 Tampermonkey，再装仓库里 <span className="mono">tools/采集助手/x-studio-采集助手.user.js</span>（详见同目录 README）。</div>
            <div>2. 在左边新建令牌，把地址、令牌、店铺 id <span className="mono">{shopId}</span> 和品类填进脚本菜单的设置里。</div>
            <div>3. 打开万相台关键词工具、淘宝搜索结果、淘宝商品页或生意参谋商品页，点右下角「发送到 X Studio」。</div>
            <div className="t3 fs-12">服务器不登录淘宝；碰到验证码脚本会停下，你自己处理后再点。</div>
          </div>
        </section>
        <section className="panel">
          <div className="panel-head"><div className="titles"><div className="panel-title">店铺日预算</div><div className="panel-sub">新品期建议的预算上限按它和 2 个毛利取小</div></div></div>
          <div className="panel-pad row" style={{ gap: 8 }}>
            <input className="input mono" style={{ width: 120 }} inputMode="decimal" placeholder="不设" value={budget} onChange={e => setBudget(e.target.value)} /><span className="t3">元 / 天</span>
            <button className="btn btn-secondary btn-sm" onClick={saveBudget}><Save />保存</button>
          </div>
        </section>
      </div>
      {creating && (
        <Drawer title="新建采集令牌" width={420} onClose={() => { setCreating(false); setPlain('') }} foot={plain ? <button className="btn btn-primary" onClick={() => { setCreating(false); setPlain('') }}>我已经存好了</button> : <><button className="btn btn-secondary" onClick={() => setCreating(false)}>取消</button><button className="btn btn-primary" onClick={create}>新建</button></>}>
          {plain ? (
            <div className="stack">
              <div className="status-note">令牌只显示这一次，关掉就再也看不到了。马上复制到采集助手里。</div>
              <div className="row" style={{ gap: 6 }}><input className="input mono grow" readOnly value={plain} onFocus={e => e.target.select()} /><button className="btn btn-secondary" onClick={() => navigator.clipboard?.writeText(plain).then(() => toast('已复制', 'ok'))}><Copy />复制</button></div>
            </div>
          ) : <Field label="名字" hint="用在哪台电脑、哪个浏览器，方便以后吊销"><input className="input" autoFocus value={name} onChange={e => setName(e.target.value)} onKeyDown={e => e.key === 'Enter' && create()} /></Field>}
        </Drawer>
      )}
    </div>
  )
}
