import { useEffect, useState } from 'react'
import { api, qs, type Page } from '../../lib/api'
import type { AuditRow, UserRow } from '../../lib/types'
import { Loading, Empty } from '../../components/ui'
import { fmtDate } from '../../lib/format'

// 动作码 → 中文。没列到的直接显示动作码。
const ACTION: Record<string, string> = {
  'login.success': '登录', 'login.failed': '登录失败',
  'user.create': '新建账号', 'user.update': '修改账号', 'user.shops': '改分店', 'quota.update': '改 AI 额度',
  'role.create': '新建角色', 'role.update': '修改角色权限', 'role.delete': '删除角色',
  'shop.create': '新建店铺', 'shop.update': '修改店铺', 'settings.update': '改系统设置',
  'product.create': '新建商品', 'product.update': '修改商品', 'product.delete': '删除商品',
  'listing.create': '新建上架版本', 'listing.update': '修改上架版本', 'listing.delete': '删除上架版本',
  'asset.delete': '删除素材', 'asset.source_override': '改判素材来源',
  'share.create': '分享给协作者', 'share.delete': '取消分享',
  'export.run': '下载成品', 'export.no_ai_label': '取消 AI 标识'
}
const PAGE = 50

export function AuditTab() {
  const [rows, setRows] = useState<AuditRow[] | null>(null)
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [userId, setUserId] = useState('')
  const [action, setAction] = useState('')
  const [users, setUsers] = useState<UserRow[]>([])
  useEffect(() => { api<Page<UserRow>>('/api/users?page_size=200').then(r => setUsers(r.items)).catch(() => {}) }, [])
  useEffect(() => {
    setRows(null)
    api<Page<AuditRow>>('/api/audit' + qs({ user_id: userId, action, page, page_size: PAGE }))
      .then(r => { setRows(r.items); setTotal(r.total) }).catch(() => setRows([]))
  }, [userId, action, page])
  const who = (id: number) => { const u = users.find(x => x.id === id); return u ? u.display_name || u.username : id ? `#${id}` : '—' }
  const pages = Math.max(1, Math.ceil(total / PAGE))
  return (
    <section className="panel">
      <div className="panel-head">
        <div className="titles"><div className="panel-title">操作日志</div><div className="panel-sub mono">{total} 条</div></div>
        <span className="grow" />
        <select className="input" style={{ width: 140 }} value={userId} onChange={e => { setUserId(e.target.value); setPage(1) }}>
          <option value="">所有人</option>{users.map(u => <option key={u.id} value={u.id}>{u.display_name || u.username}</option>)}
        </select>
        <select className="input" style={{ width: 160 }} value={action} onChange={e => { setAction(e.target.value); setPage(1) }}>
          <option value="">所有动作</option>{Object.entries(ACTION).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
        </select>
      </div>
      {!rows ? <Loading /> : rows.length === 0 ? <Empty title="没有记录" /> : (
        <div className="table-wrap"><table className="table">
          <thead><tr><th>时间</th><th>谁</th><th>做了什么</th><th>对象</th><th>详情</th><th>IP</th></tr></thead>
          <tbody>{rows.map(r => (
            <tr key={r.id}>
              <td className="mono fs-12">{fmtDate(r.at, true)}</td>
              <td>{who(r.user_id)}</td>
              <td>{ACTION[r.action] || <span className="mono">{r.action}</span>}</td>
              <td className="mono fs-12">{r.target_type ? `${r.target_type} #${r.target_id}` : '—'}</td>
              <td className="fs-12 t3 ellipsis" style={{ maxWidth: 320 }} title={JSON.stringify(r.detail)}>{r.detail && JSON.stringify(r.detail) !== '{}' ? JSON.stringify(r.detail) : ''}</td>
              <td className="mono fs-12 t3">{r.ip}</td>
            </tr>
          ))}</tbody>
        </table></div>
      )}
      {pages > 1 && (
        <div className="panel-foot row between">
          <span className="t3 fs-12">第 {page} / {pages} 页</span>
          <div className="btn-group"><button className="btn btn-secondary btn-sm" disabled={page <= 1} onClick={() => setPage(p => p - 1)}>上一页</button>
            <button className="btn btn-secondary btn-sm" disabled={page >= pages} onClick={() => setPage(p => p + 1)}>下一页</button></div>
        </div>
      )}
    </section>
  )
}
