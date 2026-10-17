import { Navigate, Route, Routes, useLocation } from 'react-router-dom'
import { useAuth } from './lib/auth'
import { Shell } from './components/Shell'
import { Loading, Topbar, Empty } from './components/ui'
import { Login } from './pages/Login'
import { Dashboard } from './pages/Dashboard'
import { Products } from './pages/Products'
import { ProductDetail } from './pages/ProductDetail'
import { Admin } from './pages/Admin'
import { SettingsPage } from './pages/Settings'
import { Assets } from './pages/Assets'
import { Workbench } from './pages/Workbench'

// 还在开发的模块：后端由并行的 AI 在做，合并后再接页面
function Soon({ title, desc }: { title: string; desc: string }) {
  return (
    <>
      <Topbar crumbs={[title]} />
      <div className="scroll"><main className="content"><Empty title={`${title}正在开发`} desc={desc} /></main></div>
    </>
  )
}

export function App() {
  const { me, loading } = useAuth()
  const loc = useLocation()
  if (loc.pathname === '/login') return <Login />
  if (loading) return <Loading />
  if (!me) return <Navigate to={'/login?next=' + encodeURIComponent(loc.pathname + loc.search)} replace />
  return (
    <Routes>
      <Route element={<Shell />}>
        <Route index element={<Dashboard />} />
        <Route path="products" element={<Products />} />
        <Route path="products/:id" element={<ProductDetail />} />
        <Route path="assets" element={<Assets />} />
        <Route path="process" element={<Workbench />} />
        <Route path="copy" element={<Soon title="文案中心" desc="文案版本、AI 改写、合规检查、关键词库正在开发。" />} />
        <Route path="templates" element={<Soon title="模板中心" desc="5 套详情页模板已就绪，选模板和预览的后端正在开发。" />} />
        <Route path="review" element={<Soon title="出图审核" desc="出图审核和导出的后端正在开发。" />} />
        <Route path="admin" element={<Admin />} />
        <Route path="settings" element={<SettingsPage />} />
        <Route path="*" element={<Soon title="页面不存在" desc="检查一下地址，或者从左侧导航进入。" />} />
      </Route>
    </Routes>
  )
}
