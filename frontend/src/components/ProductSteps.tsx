import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { Check } from 'lucide-react'
import { useAuth } from '../lib/auth'
import type { Asset, Product } from '../lib/types'
import { creativeApi, itemOk, mockOn, SET_STATUS, type CreativeSet } from '../lib/creative'
import { sceneApi, type Binding } from '../lib/scenes'

type Tab = 'assets' | 'listings' | 'creative' | 'info' | 'stock' | 'tasks'
type State = 'done' | 'doing' | 'todo'

// 商品详情顶上的上新进度：照片 → 场景 → 文案 → AI 出图 → 预览导出。只看第一个店铺版本（多数款只有一个店）。
export function ProductSteps({ product, photos, onTab }: { product: Product; photos: Asset[]; onTab: (t: Tab) => void }) {
  const { can, me } = useAuth()
  const listing = product.listings[0]
  const [set, setSet] = useState<CreativeSet | null | undefined>(undefined)
  const [scene, setScene] = useState<Binding | null>(null)
  const canCreative = can('creative.run') && me?.role.scope !== 'shares'
  useEffect(() => {
    if (!listing || !canCreative) return
    creativeApi.sets(listing.id).then(r => setSet(r.items?.[0] || null)).catch(() => setSet(null))
    if (!mockOn()) sceneApi.bindings([listing.id]).then(r => setScene(r.listings[0] || null)).catch(() => setScene(null))
  }, [listing, canCreative])
  if (!listing) return null

  const images = photos.filter(a => a.media !== 'video').length
  const videos = photos.length - images
  const title = (listing.meta as { title?: string } | undefined)?.title
  const items = (set?.items || []).filter(it => !it.superseded)
  const waiting = items.filter(it => !it.human && it.route === 'manual').length
  const ok = items.filter(itemOk).length
  const steps: { key: string; label: string; state: State; text: string; go?: () => void; to?: string }[] = [
    { key: 'photo', label: '照片', state: images ? 'done' : 'doing', text: images ? `${images} 张照片${videos ? ` · ${videos} 个视频` : ''}` : '还没传', go: () => onTab('assets') },
    { key: 'scene', label: '场景', state: scene && scene.source !== 'none' ? 'done' : 'todo', text: scene && scene.source !== 'none' ? `${scene.scene_name}${scene.source === 'shop' ? '（店铺默认）' : scene.source === 'default' ? '（默认）' : ''}` : '没选，用默认', to: '/scenes' },
    { key: 'copy', label: '文案', state: title ? 'done' : 'todo', text: title ? '已写' : '还没写', to: `/copy?listing=${listing.id}` },
    { key: 'creative', label: 'AI 出图', state: !set ? 'todo' : set.status === 'ready' || set.status === 'exported' ? 'done' : 'doing', text: !set ? '还没出' : waiting ? `${ok} 张可用 · ${waiting} 张等你看` : SET_STATUS[set.status]?.label || set.status, go: () => onTab('creative') },
    { key: 'export', label: '预览导出', state: set?.status === 'exported' ? 'done' : 'todo', text: set?.status === 'exported' ? '已导出' : set ? '看完再导出' : '—', go: () => onTab('creative') }
  ]
  if (!canCreative) steps.splice(1, 1)
  return (
    <ol className="psteps">
      {steps.map((s, i) => {
        const body = <>
          <span className={'psteps-dot ' + s.state}>{s.state === 'done' ? <Check /> : i + 1}</span>
          <span className="psteps-text"><span className="psteps-label">{s.label}</span><span className="psteps-sub ellipsis">{s.text}</span></span>
        </>
        return <li key={s.key} className={'psteps-item ' + s.state}>
          {s.to ? <Link to={s.to} className="psteps-btn">{body}</Link> : <button type="button" className="psteps-btn" onClick={s.go}>{body}</button>}
        </li>
      })}
    </ol>
  )
}
