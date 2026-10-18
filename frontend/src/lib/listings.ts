import { useEffect, useState } from 'react'
import { api, type Page } from './api'
import type { Listing, Product } from './types'

// 文案、模板页要按「款 × 店铺」挑上架版本：把商品列表摊平成上架版本列表（不含已归档的款）
export type ListingRef = { id: number; shop_id: number; listing: Listing; product: Product }

export function useListingRefs() {
  const [refs, setRefs] = useState<ListingRef[] | null>(null)
  useEffect(() => {
    api<Page<Product>>('/api/products?page_size=200')
      .then(r => setRefs(r.items.filter(p => p.status !== 'archived').flatMap(p => (p.listings || []).map(l => ({ id: l.id, shop_id: l.shop_id, listing: l, product: p })))))
      .catch(() => setRefs([]))
  }, [])
  return refs
}
