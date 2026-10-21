import { useCallback, useEffect, useState } from 'react'
import { api } from './api'
import type { AttrField, Category } from './types'

// 品类很少变，整个页面共用一份；管理页改完调用 reload 刷新
let cache: Category[] | null = null
let pending: Promise<Category[]> | null = null
const listeners = new Set<(c: Category[]) => void>()

function fetchAll(force = false) {
  if (cache && !force) return Promise.resolve(cache)
  if (!pending || force) {
    pending = api<{ items: Category[] }>('/api/categories?all=1')
      .then(r => { cache = r.items || []; listeners.forEach(f => f(cache!)); return cache })
      .finally(() => { pending = null })
  }
  return pending
}

// 默认只给启用的；all=true 时包括停用的（管理页、显示老商品的品类名用）
export function useCategories(all = false) {
  const [items, setItems] = useState<Category[] | null>(cache)
  useEffect(() => {
    listeners.add(setItems)
    fetchAll().catch(() => setItems([]))
    return () => { listeners.delete(setItems) }
  }, [])
  const reload = useCallback(() => fetchAll(true).then(() => undefined), [])
  const list = items ? (all ? items : items.filter(c => !c.disabled)) : null
  return { items: list, reload }
}

export const emptyValue = (v: unknown) => v === undefined || v === null || v === '' || (Array.isArray(v) && v.length === 0)

// 必填但没填的字段名
export const missingAttrs = (attrs: AttrField[], info: Record<string, unknown>) =>
  attrs.filter(a => a.required && emptyValue(info[a.key])).map(a => a.label)

export function attrText(a: AttrField | undefined, v: unknown) {
  if (Array.isArray(v)) return v.join('、')
  if (typeof v === 'boolean') return v ? '是' : '否'
  const s = String(v)
  return a?.unit && a.type !== 'number' && !s.endsWith(a.unit) ? `${s} ${a.unit}` : s
}

// 拍摄编号的中文名：A1 → 平铺正面；C4_8mm → 同一位置多尺寸 · 8mm；A1_AI → 平铺正面（AI 补）。
// 先找指定品类，找不到再找其他品类；都没有就原样返回编号。
export function codeName(cats: Category[] | null | undefined, code: string, categoryId?: number | string) {
  if (!code) return ''
  const [base, ...rest] = code.split('_')
  const suffix = rest.join('_')
  const list = [...(cats || [])].sort((a, b) => (String(a.id) === String(categoryId) ? -1 : 0) - (String(b.id) === String(categoryId) ? -1 : 0))
  const hit = list.flatMap(c => c.codes || []).find(c => c.code === base)
  if (!hit) return code
  if (!suffix) return hit.name
  if (suffix === 'AI') return `${hit.name}（AI 补）`
  return `${hit.name} · ${suffix}`
}

export function useCodeName() {
  const { items } = useCategories(true)
  return useCallback((code: string, categoryId?: number | string) => codeName(items, code, categoryId), [items])
}
