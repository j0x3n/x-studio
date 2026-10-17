import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Package, AlertTriangle, Sparkles, Image as ImageIcon, ChevronRight } from 'lucide-react'
import { api, type Page } from '../lib/api'
import { useAuth } from '../lib/auth'
import type { Product } from '../lib/types'
import { Topbar, PageHead, Pill, Loading, Empty } from '../components/ui'
import { fmtAgo, productStatus, pipelineStatus } from '../lib/format'

type Usage = { items: { kind: string; units: number; est_cost: number }[] }

export function Dashboard() {
  const { me, can } = useAuth()
  const nav = useNavigate()
  const [products, setProducts] = useState<Product[] | null>(null)
  const [usage, setUsage] = useState<Usage | null>(null)
  useEffect(() => {
    if (can('product.view')) api<Page<Product>>('/api/products?page_size=200').then(r => setProducts(r.items)).catch(() => setProducts([]))
    else setProducts([])
    api<Usage>('/api/usage/me').then(setUsage).catch(() => setUsage(null))
  }, [can])

  if (!products) return <><Topbar crumbs={['工作台']} /><Loading /></>
  const count = (s: string) => products.filter(p => p.status === s).length
  const failed = products.filter(p => p.listings?.some(l => l.pipeline_status === 'failed'))
  const noPhoto = products.filter(p => !p.asset_count && p.status !== 'archived')
  const units = (k: string) => (usage?.items || []).filter(i => i.kind === k).reduce((a, b) => a + (b.units || 0), 0)
  const recent = [...products].sort((a, b) => b.updated_at.localeCompare(a.updated_at)).slice(0, 8)
  const name = me?.user.display_name || me?.user.username

  return (
    <>
      <Topbar crumbs={['工作台']} />
      <div className="scroll"><main className="content">
        <PageHead title={`你好，${name}`} sub={<>{me?.role.name}<span className="sep" />{me?.shops.map(s => s.name).join('、') || '没有分到店铺'}</>} />
        <section className="kpi-grid">
          <div className="kpi">
            <div className="kpi-top"><span className="kpi-label">商品</span><span className="icon-tile sm"><Package /></span></div>
            <div className="kpi-value mono">{products.length}</div>
            <div className="kpi-foot">拍样中 {count('sampling')} · 待上架 {count('pending')} · 已上架 {count('listed')}</div>
          </div>
          <div className="kpi">
            <div className="kpi-top"><span className="kpi-label">出图失败</span><span className="icon-tile sm tint-red"><AlertTriangle /></span></div>
            <div className="kpi-value mono">{failed.length}</div>
            <div className="kpi-foot">需要看原因后重跑</div>
          </div>
          <div className="kpi">
            <div className="kpi-top"><span className="kpi-label">还没传照片</span><span className="icon-tile sm tint-orange"><ImageIcon /></span></div>
            <div className="kpi-value mono">{noPhoto.length}</div>
            <div className="kpi-foot">未归档的商品</div>
          </div>
          <div className="kpi">
            <div className="kpi-top"><span className="kpi-label">我本月的 AI 用量</span><span className="icon-tile sm tint-purple"><Sparkles /></span></div>
            <div className="kpi-value mono">{units('image')}<span className="t3" style={{ fontSize: 13, fontWeight: 400 }}> 张图</span></div>
            <div className="kpi-foot">文本调用 {units('text')} 次</div>
          </div>
        </section>

        <div className="two-col">
          <section className="panel">
            <div className="panel-head"><div className="titles"><div className="panel-title">最近更新的商品</div></div>
              <span className="grow" /><button className="btn btn-ghost btn-sm" onClick={() => nav('/products')}>全部商品<ChevronRight /></button></div>
            {recent.length === 0 ? <Empty title="还没有商品" desc="去商品库新建第一款。" /> : (
              <div className="table-wrap"><table className="table">
                <thead><tr><th>商品</th><th>状态</th><th>出图</th><th>更新</th></tr></thead>
                <tbody>{recent.map(p => {
                  const ps = productStatus[p.status]
                  const failedL = p.listings?.find(l => l.pipeline_status === 'failed')
                  const pl = pipelineStatus[failedL?.pipeline_status || p.listings?.[0]?.pipeline_status || 'draft']
                  return (
                    <tr key={p.id} className="clickable" onClick={() => nav(`/products/${p.id}`)}>
                      <td><div className="name-cell">{p.cover_url ? <img className="cover" src={p.cover_url} alt="" /> : <span className="cover"><Package /></span>}
                        <div><div className="cell-title">{p.name}</div><div className="cell-sub mono">#{p.id} · {p.asset_count ?? 0} 张素材</div></div></div></td>
                      <td><Pill kind={ps?.pill || 'pill-neutral'}>{ps?.label || p.status}</Pill></td>
                      <td>{pl && <Pill kind={pl.pill}>{pl.label}</Pill>}</td>
                      <td className="t3">{fmtAgo(p.updated_at)}</td>
                    </tr>)
                })}</tbody>
              </table></div>
            )}
          </section>
          <section className="panel">
            <div className="panel-head"><div className="titles"><div className="panel-title">要处理的</div><div className="panel-sub">出图失败和缺照片的商品</div></div></div>
            {failed.length + noPhoto.length === 0 ? <Empty title="暂时没有要处理的" /> : (
              <div className="list todo-list">
                {[...failed.map(p => ({ p, bad: true })), ...noPhoto.map(p => ({ p, bad: false }))].slice(0, 10).map(({ p, bad }) => {
                  const err = bad ? p.listings?.find(l => l.pipeline_status === 'failed')?.pipeline_error : ''
                  return (
                    <a key={(bad ? 'f' : 'n') + p.id} className="list-item" onClick={() => nav(`/products/${p.id}`)}>
                      <span className={'icon-tile sm ' + (bad ? 'tint-red' : 'tint-orange')}>{bad ? <AlertTriangle /> : <ImageIcon />}</span>
                      <div className="grow" style={{ minWidth: 0 }}>
                        <div className="title ellipsis">{p.name}</div>
                        <div className="meta ellipsis" title={err || undefined}>{bad ? `出图失败${err ? '：' + err : ''}` : '还没传照片'}</div>
                      </div>
                      <ChevronRight className="end t4" />
                    </a>
                  )
                })}
              </div>
            )}
          </section>
        </div>
      </main></div>
    </>
  )
}
