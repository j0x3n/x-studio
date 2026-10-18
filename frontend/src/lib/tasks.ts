import type { TaskPriority, TaskStatus } from './types'

export const taskStatus: Record<TaskStatus, { label: string; pill: string }> = {
  todo: { label: '待办', pill: 'pill-neutral' },
  doing: { label: '进行中', pill: 'pill-info' },
  waiting: { label: '等待', pill: 'pill-warning' },
  done: { label: '完成', pill: 'pill-success' },
  canceled: { label: '取消', pill: 'pill-neutral' }
}
export const taskPriority: Record<TaskPriority, { label: string; pill: string }> = {
  low: { label: '低', pill: 'pill-neutral' },
  normal: { label: '普通', pill: 'pill-neutral' },
  high: { label: '高', pill: 'pill-warning' },
  urgent: { label: '紧急', pill: 'pill-danger' }
}
export const OPEN: TaskStatus[] = ['todo', 'doing', 'waiting']

export const noticeType: Record<string, string> = {
  task_assigned: '派给我的任务', task_mentioned: '@我', task_comment: '任务有新评论', task_due: '任务快到期', task_overdue: '任务逾期', task_unassigned: '任务没派出去',
  task_done: '我建的任务完成了', job_failed: '后台任务失败', job_done: '后台任务完成', review_result: '审核结果', quota_80: 'AI 额度快用完'
}

export const ruleEvent: Record<string, string> = {
  'product.created': '新建商品', 'assets.first_upload': '商品第一次上传素材', 'render.failed': '一键出图失败', 'review.rejected': '出图审核被退回'
}
export const assignMode: Record<string, string> = {
  fixed: '固定的人', role_round_robin: '按角色轮流', role_least_loaded: '按角色派给最闲的', product_owner: '商品创建人', job_user: '出图发起人'
}

// datetime-local 和后端时间互转
export const toLocalInput = (iso: string) => {
  if (!iso) return ''
  const d = new Date(iso)
  if (isNaN(d.getTime())) return ''
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`
}
export const fromLocalInput = (v: string) => (v ? new Date(v).toISOString() : '')

export function dueText(iso: string) {
  if (!iso) return ''
  const d = new Date(iso), now = new Date()
  const day = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime()
  const diff = Math.round((day(d) - day(now)) / 86400000)
  const hm = `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
  if (diff === 0) return `今天 ${hm}`
  if (diff === 1) return `明天 ${hm}`
  if (diff === -1) return `昨天 ${hm}`
  return `${d.getMonth() + 1}月${d.getDate()}日`
}

// 时间线一条记录的文字。kind 和 detail 由后端定，认不出的显示原文
const FIELD: Record<string, string> = { title: '标题', body: '说明', priority: '优先级', due_at: '截止时间', assignee_id: '负责人', target: '对象', watchers: '协作人', checklist: '检查清单' }
export function eventText(kind: string, d: Record<string, unknown> = {}) {
  const v = (x: unknown) => (x === null || x === undefined || x === '' ? '空' : String(x))
  const st = (x: unknown) => taskStatus[x as TaskStatus]?.label || v(x)
  switch (kind) {
    case 'created': return d.rule_id ? '按自动派活规则建了任务' : '建了任务'
    case 'status': return `把状态从「${st(d.from)}」改成「${st(d.to)}」`
    case 'assignee': return `把负责人从 ${d.from ? v(d.from_name ?? d.from) : '没派'} 改成 ${d.to ? v(d.to_name ?? d.to) : '没派'}`
    case 'due_at': case 'due': return d.to ? `把截止时间改成 ${dueText(String(d.to))}` : '去掉了截止时间'
    case 'priority': return `把优先级改成「${taskPriority[d.to as TaskPriority]?.label || v(d.to)}」`
    case 'repeat': return `规则又触发了一次${d.reason ? `：${d.reason}` : ''}`
    case 'reopen': return '重新打开了任务'
    default: {
      const f = (d.fields as string[] | undefined)?.map(k => FIELD[k] || k).join('、')
      return f ? `改了${f}` : kind
    }
  }
}
