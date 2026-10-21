import { useCallback, useEffect, useState } from 'react'
import { Plus, Store, Radio } from 'lucide-react'
import { api, type Page } from '../../lib/api'
import { useAuth } from '../../lib/auth'
import { useToast, errText } from '../../lib/toast'
import type { Shop } from '../../lib/types'
import { Pill, Loading, Drawer, Field } from '../../components/ui'
import { PLATFORMS, SITES, SITE_CURRENCY, CURRENCIES, LANGUAGES, platformOf, shopLabel } from '../../lib/shops'
import { creativeApi, type ShopCreative } from '../../lib/creative'
import { sceneApi, type Scene } from '../../lib/scenes'

// 店铺和渠道：同一平台可以开多个店（淘宝 A 店、淘宝 B 店、虾皮台湾、虾皮马来），直播间也算一个渠道。
// 每个店有自己的文案语言、币种、主图比例、详情图宽度、默认场景和「关于我们」。
export function ShopsTab() {
  const { refresh } = useAuth()
  const [shops, setShops] = useState<Shop[] | null>(null)
  const [edit, setEdit] = useState<Shop | 'new' | null>(null)
  const load = useCallback(() => { api<Page<Shop>>('/api/shops?page_size=200').then(r => setShops(r.items)).catch(() => setShops([])) }, [])
  useEffect(load, [load])
  if (!shops) return <Loading />
  return (
    <section className="panel">
      <div className="panel-head">
        <div className="titles"><div className="panel-title">店铺和渠道</div><div className="panel-sub">同一平台可以开多个店，直播间也算一个渠道。每个店有自己的语言、币种和出图规格；不能删，只能归档。</div></div>
        <span className="grow" />
        <button className="btn btn-primary btn-sm" onClick={() => setEdit('new')}><Plus />新建店铺或渠道</button>
      </div>
      <div className="table-wrap"><table className="table">
        <thead><tr><th>名称</th><th>平台 / 站点</th><th>语言</th><th>币种</th><th>状态</th></tr></thead>
        <tbody>{shops.map(s => (
          <tr key={s.id} className="clickable" onClick={() => setEdit(s)}>
            <td><div className="cell-person"><span className="icon-tile sm">{s.platform === 'live' ? <Radio /> : <Store />}</span><div><div className="cell-title">{s.name}</div><div className="cell-sub mono">#{s.id}</div></div></div></td>
            <td>{shopLabel(s)}</td>
            <td>{LANGUAGES[s.language] || s.language}</td>
            <td className="mono">{s.currency || '—'}</td>
            <td>{s.archived ? <Pill kind="pill-neutral">已归档</Pill> : <Pill kind="pill-success">使用中</Pill>}</td>
          </tr>
        ))}</tbody>
      </table></div>
      {edit && <ShopDrawer shop={edit === 'new' ? null : edit} onClose={() => setEdit(null)} onDone={() => { setEdit(null); load(); refresh() }} />}
    </section>
  )
}

const RATIOS = [['3x4', '3:4 竖图'], ['1x1', '1:1 方图']] as const

function ShopDrawer({ shop, onClose, onDone }: { shop: Shop | null; onClose: () => void; onDone: () => void }) {
  const toast = useToast()
  const { can } = useAuth()
  const legacy = shop?.platform === 'shopee_tw'
  const [name, setName] = useState(shop?.name || '')
  const [platform, setPlatform] = useState<string>(legacy ? 'shopee' : shop?.platform || 'taobao')
  const [site, setSite] = useState(legacy ? 'tw' : shop?.site || '')
  const [currency, setCurrency] = useState(shop?.currency || (legacy ? 'TWD' : shop ? '' : 'CNY'))
  const [language, setLanguage] = useState(shop?.language || 'zh-CN')
  const [archived, setArchived] = useState(!!shop?.archived)
  const [busy, setBusy] = useState(false)
  // 出图设置（creative 店铺设置 + 场景库的店铺默认），只有建好的店能改
  const creativeEdit = !!shop && can('creative.template')
  const [cfg, setCfg] = useState<ShopCreative | null>(null)
  const [scenes, setScenes] = useState<Scene[]>([])
  const [scene, setScene] = useState(0)
  const [scene0, setScene0] = useState(0)
  useEffect(() => {
    if (!creativeEdit || !shop) return
    creativeApi.shop(shop.id).then(setCfg).catch(() => setCfg(null))
    sceneApi.list({ shop_id: shop.id }).then(setScenes).catch(() => setScenes([]))
  }, [creativeEdit, shop])
  useEffect(() => {
    if (!creativeEdit || !shop) return
    sceneApi.shopBinding(shop.id).then(id => { setScene(id || 0); setScene0(id || 0) }).catch(() => {})
  }, [creativeEdit, shop])

  const p = platformOf(platform)
  const pickPlatform = (k: string) => {
    setPlatform(k)
    if (!platformOf(k)?.sites) setSite('')
    if (k === 'taobao' || k === 'tmall' || k === 'pdd' || k === 'douyin' || k === 'xiaohongshu') { setCurrency('CNY'); setLanguage('zh-CN') }
  }
  const pickSite = (k: string) => { setSite(k); if (SITE_CURRENCY[k]) setCurrency(SITE_CURRENCY[k]); if (k === 'tw') setLanguage('zh-TW') }
  const save = async () => {
    if (p?.sites && !site) return toast(`${p.name}要选站点`, 'error')
    setBusy(true)
    try {
      const body = { name: name.trim(), platform, site: p?.sites ? site : '', currency, language, archived }
      if (shop) await api(`/api/shops/${shop.id}`, { method: 'PATCH', body })
      else await api('/api/shops', { body })
      if (shop && creativeEdit && cfg) {
        await creativeApi.saveShop(shop.id, cfg)
        if (scene !== scene0) await sceneApi.bind({ shop_id: shop.id, scene_id: scene || null })
      }
      toast('已保存', 'ok'); onDone()
    } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  const ratios = cfg?.main_ratios || []
  const toggleRatio = (k: string) => cfg && setCfg({ ...cfg, main_ratios: ratios.includes(k) ? ratios.filter(x => x !== k) : [...ratios, k] })
  return (
    <Drawer title={shop ? `编辑 · ${shop.name}` : '新建店铺或渠道'} width={480} onClose={onClose} foot={<>
      <button className="btn btn-secondary" onClick={onClose}>取消</button>
      <button className="btn btn-primary" disabled={busy || !name.trim()} onClick={save}>保存</button>
    </>}>
      <Field label="名称" req hint="同平台开了几个店，用名字区分"><input className="input" value={name} onChange={e => setName(e.target.value)} placeholder={platform === 'live' ? '如：抖音直播间（晚场）' : '如：淘宝 · 珠光秘境旗舰、虾皮马来店'} /></Field>
      <div className="form-row">
        <Field label="平台"><select className="input" value={platform} onChange={e => pickPlatform(e.target.value)}>
          {PLATFORMS.map(x => <option key={x.key} value={x.key}>{x.name}</option>)}</select></Field>
        {p?.sites ? <Field label="站点" req><select className="input" value={site} onChange={e => pickSite(e.target.value)}>
          <option value="">选站点</option>{Object.entries(SITES).map(([k, v]) => <option key={k} value={k}>{v}</option>)}</select></Field> : <span />}
      </div>
      <div className="form-row">
        <Field label="文案语言"><select className="input" value={language} onChange={e => setLanguage(e.target.value)}>
          {Object.entries(LANGUAGES).map(([k, v]) => <option key={k} value={k}>{v}</option>)}</select></Field>
        <Field label="币种"><select className="input mono" value={currency} onChange={e => setCurrency(e.target.value)}>
          <option value="">不填</option>{CURRENCIES.map(c => <option key={c} value={c}>{c}</option>)}</select></Field>
      </div>
      {platform === 'live' && <div className="help">直播间这次先能挂商品；直播背景、讲解卡、口播稿下一轮和 AI 主视频一起做。</div>}

      {creativeEdit && <>
        <div className="form-section-title">出图</div>
        {!cfg ? <Loading /> : <>
          <div className="form-field"><span className="label">主图导出比例<span className="help">都不勾 = 按模板（3:4 和 1:1 都出）</span></span>
            <div className="inline">{RATIOS.map(([k, l]) => <label key={k} className="filter-opt"><input type="checkbox" className="check" checked={ratios.includes(k)} onChange={() => toggleRatio(k)} />{l}</label>)}</div>
          </div>
          <Field label="详情图宽度（像素）" hint="0 = 按模板；淘宝常用 750，天猫 790">
            <input className="input mono" type="number" min={0} max={1500} step={10} value={cfg.detail_width || 0} onChange={e => setCfg({ ...cfg, detail_width: Math.max(0, Math.min(1500, Number(e.target.value) || 0)) })} />
          </Field>
          <Field label="默认场景" hint="这个店没单独指定场景的款都用它">
            <select className="input" value={scene} onChange={e => setScene(Number(e.target.value))}>
              <option value={0}>不设（用全局默认）</option>
              {scenes.map(s => <option key={s.id} value={s.id}>{s.name}{s.shop_id ? '' : '（通用）'}</option>)}
            </select>
          </Field>
          <Field label="「关于我们」写什么" hint="详情页最后一屏的三行字；空着就按品类里的写法">
            <textarea className="textarea" rows={3} maxLength={300} value={cfg.about_text || ''} onChange={e => setCfg({ ...cfg, about_text: e.target.value })} placeholder={'珠光秘境，做珍珠和水晶的小饰品。\n每一款都是我们自己挑、自己拍、自己打包。'} />
          </Field>
        </>}
      </>}

      {shop && (
        <label className="setting" style={{ cursor: 'pointer' }}>
          <div><div className="cell-title">归档</div><div className="fs-12 t3">归档后不能再新建上架版本，已有数据保留。</div></div>
          <input type="checkbox" className="switch" checked={archived} onChange={e => setArchived(e.target.checked)} />
        </label>
      )}
    </Drawer>
  )
}
