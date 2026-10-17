import { useCallback, useEffect, useState } from 'react'
import { Plus, Store } from 'lucide-react'
import { api, type Page } from '../../lib/api'
import { useAuth } from '../../lib/auth'
import { useToast, errText } from '../../lib/toast'
import type { Shop } from '../../lib/types'
import { Pill, Loading, Drawer, Field } from '../../components/ui'
import { platformName, languageName } from '../../lib/format'

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
        <div className="titles"><div className="panel-title">店铺</div><div className="panel-sub">每个店铺有自己的上架版本、文案语言和出图规格。店铺不能删，只能归档。</div></div>
        <span className="grow" />
        <button className="btn btn-primary btn-sm" onClick={() => setEdit('new')}><Plus />新建店铺</button>
      </div>
      <div className="table-wrap"><table className="table">
        <thead><tr><th>店铺</th><th>平台</th><th>文案语言</th><th>状态</th></tr></thead>
        <tbody>{shops.map(s => (
          <tr key={s.id} className="clickable" onClick={() => setEdit(s)}>
            <td><div className="cell-person"><span className="icon-tile sm"><Store /></span><div><div className="cell-title">{s.name}</div><div className="cell-sub mono">#{s.id}</div></div></div></td>
            <td>{platformName[s.platform] || s.platform}</td>
            <td>{languageName[s.language] || s.language}</td>
            <td>{s.archived ? <Pill kind="pill-neutral">已归档</Pill> : <Pill kind="pill-success">使用中</Pill>}</td>
          </tr>
        ))}</tbody>
      </table></div>
      {edit && <ShopDrawer shop={edit === 'new' ? null : edit} onClose={() => setEdit(null)} onDone={() => { setEdit(null); load(); refresh() }} />}
    </section>
  )
}

function ShopDrawer({ shop, onClose, onDone }: { shop: Shop | null; onClose: () => void; onDone: () => void }) {
  const toast = useToast()
  const [name, setName] = useState(shop?.name || '')
  const [platform, setPlatform] = useState<string>(shop?.platform || 'taobao')
  const [language, setLanguage] = useState(shop?.language || 'zh-CN')
  const [archived, setArchived] = useState(!!shop?.archived)
  const [busy, setBusy] = useState(false)
  const save = async () => {
    setBusy(true)
    try {
      const body = { name: name.trim(), platform, language, archived }
      if (shop) await api(`/api/shops/${shop.id}`, { method: 'PATCH', body })
      else await api('/api/shops', { body })
      toast('已保存', 'ok'); onDone()
    } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }
  return (
    <Drawer title={shop ? `编辑店铺 · ${shop.name}` : '新建店铺'} width={380} onClose={onClose} foot={<>
      <button className="btn btn-secondary" onClick={onClose}>取消</button>
      <button className="btn btn-primary" disabled={busy || !name.trim()} onClick={save}>保存</button>
    </>}>
      <Field label="店铺名" req><input className="input" value={name} onChange={e => setName(e.target.value)} placeholder="如：虾皮台湾店" /></Field>
      <div className="form-row">
        <Field label="平台"><select className="input" value={platform} onChange={e => setPlatform(e.target.value)}>
          {Object.entries(platformName).map(([k, v]) => <option key={k} value={k}>{v}</option>)}</select></Field>
        <Field label="文案语言"><select className="input" value={language} onChange={e => setLanguage(e.target.value)}>
          {Object.entries(languageName).map(([k, v]) => <option key={k} value={k}>{v}</option>)}</select></Field>
      </div>
      {shop && (
        <label className="setting" style={{ cursor: 'pointer' }}>
          <div><div className="cell-title">归档这个店铺</div><div className="fs-12 t3">归档后不能再新建上架版本，已有数据保留。</div></div>
          <input type="checkbox" className="switch" checked={archived} onChange={e => setArchived(e.target.checked)} />
        </label>
      )}
    </Drawer>
  )
}
