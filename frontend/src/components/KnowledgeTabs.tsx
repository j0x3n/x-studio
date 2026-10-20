import { BookOpen, Camera } from 'lucide-react'
import { NavLink } from 'react-router-dom'
import { useAuth } from '../lib/auth'

export function KnowledgeTabs() {
  const { can } = useAuth()
  return <nav className="knowledge-tabs" aria-label="知识库栏目">
    {can('kb.view') && <NavLink to="/kb" end className={({ isActive }) => 'knowledge-tab' + (isActive ? ' active' : '')}><BookOpen size={15} />知识文章</NavLink>}
    <NavLink to="/kb/shooting-guide" className={({ isActive }) => 'knowledge-tab' + (isActive ? ' active' : '')}><Camera size={15} />拍摄要求</NavLink>
  </nav>
}
