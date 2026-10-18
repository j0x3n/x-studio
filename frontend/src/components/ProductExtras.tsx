import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { Plus } from 'lucide-react'
import { api, qs, type Page } from '../lib/api'
import { useAuth } from '../lib/auth'
import { fmtAgo } from '../lib/format'
import { specText, availText, yuan, type InvItem } from '../lib/inventory'
import type { KbArticle } from '../lib/kb'
import type { Product } from '../lib/types'
import { Empty, Loading } from './ui'
import { ItemDrawer, useCostPerm } from './InventoryForms'

// 商品详情「库存」标签：这款下面的成品 SKU 和各自的可售数
export function ProductStock({ product }: { product: Product }) {
  const nav = useNavigate()
  const { can } = useAuth()
  const costPerm = useCostPerm()
  const [items, setItems] = useState<InvItem[] | null>(null)
  const [adding, setAdding] = useState(false)
  const load = useCallback(() => {
    api<Page<InvItem>>('/api/inventory/items' + qs({ product_id: product.id, archived: 1, page_size: 100 })).then(r => setItems(r.items || [])).catch(() => setItems([]))
  }, [product.id])
  useEffect(() => { load() }, [load])
  return (
    <section className="panel">
      <div className="panel-head"><div className="titles"><div className="panel-title">规格和库存</div><div className="panel-sub">每个规格一个成品物料，编码就是平台上的商家编码</div></div><span className="grow" />{can('inventory.edit') && <button className="btn btn-secondary btn-sm" onClick={() => setAdding(true)}><Plus />加规格</button>}</div>
      {!items ? <Loading /> : items.length === 0 ? <Empty title="还没有建库存物料" desc={can('inventory.edit') ? '点「加规格」，按尺寸、颜色等每个规格建一个。' : undefined} /> : (
        <div className="table-wrap"><table className="table">
          <thead><tr><th>编码</th><th>名称和规格</th><th className="num">现有</th><th className="num">占用</th><th className="num">可售</th>{costPerm && <th className="num">库存成本</th>}<th>更新</th></tr></thead>
          <tbody>{items.map(it => (
            <tr key={it.id} className={'clickable' + (it.archived ? ' t4' : '')} onClick={() => nav(`/inventory/items/${it.id}`)}>
              <td className="mono">{it.code}</td>
              <td className="wrap"><div className="cell-title">{it.name}{it.archived && <span className="tag tag-gray" style={{ marginLeft: 6 }}>已归档</span>}</div>{specText(it.spec) && <div className="cell-sub">{specText(it.spec)}</div>}</td>
              <td className="num mono">{it.on_hand}</td><td className="num mono">{it.reserved || '—'}</td>
              <td className={'num mono' + (it.low ? ' danger-text' : '')}>{availText(it)}</td>
              {costPerm && <td className="num mono">{yuan(it.cost_value)}</td>}
              <td className="t3">{fmtAgo(it.updated_at)}</td>
            </tr>
          ))}</tbody>
        </table></div>
      )}
      {adding && <ItemDrawer product={{ id: product.id, name: product.name, category_id: product.category_id }} onClose={() => setAdding(false)} onDone={() => { setAdding(false); load() }} />}
    </section>
  )
}

// 商品详情「基础信息」里的相关文章：关联了这款商品或它的品类的文章
export function RelatedArticles({ product }: { product: Product }) {
  const [list, setList] = useState<KbArticle[] | null>(null)
  useEffect(() => {
    const reqs = [api<Page<KbArticle>>('/api/kb/articles' + qs({ target_type: 'product', target_id: product.id, page_size: 20 }))]
    if (product.category_id) reqs.push(api<Page<KbArticle>>('/api/kb/articles' + qs({ target_type: 'category', target_id: product.category_id, page_size: 20 })))
    Promise.all(reqs.map(r => r.then(x => x.items || []).catch(() => [] as KbArticle[])))
      .then(([a, b = []]) => setList([...a, ...b.filter(x => !a.some(y => y.id === x.id))]))
  }, [product.id, product.category_id])
  return (
    <section className="panel">
      <div className="panel-head"><div className="titles"><div className="panel-title">相关文章</div><div className="panel-sub">知识库里关联了这款或它的品类的文章</div></div><span className="grow" /><Link className="link fs-12" to="/kb">打开知识库</Link></div>
      {!list ? <Loading /> : list.length === 0 ? <div className="panel-pad t4">还没有。写文章时在「关联」里选这款商品或品类，就会出现在这里。</div> : (
        <div className="panel-pad list">{list.map(a => (
          <Link key={a.id} className="list-item" to={`/kb/${a.id}`}>
            <span className="title ellipsis">{a.title}</span><span className="meta">{a.folder?.name}</span>
            <span className="end meta">{fmtAgo(a.updated_at)}</span>
          </Link>
        ))}</div>
      )}
    </section>
  )
}
