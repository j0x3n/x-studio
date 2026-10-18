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
import { CopyCenter } from './pages/CopyCenter'
import { TemplateCenter } from './pages/TemplateCenter'
import { Review } from './pages/Review'
import { ShootingGuide } from './pages/ShootingGuide'

// 占位页（目前只用于地址不存在）
function Soon({ title, desc }: { title: string; desc: string }) {
  return (
    <>
      <Topbar crumbs={[title]} />
      <div className="scroll"><main className="content"><Empty title={title} desc={desc} /></main></div>
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
        <Route path="copy" element={<CopyCenter />} />
        <Route path="templates" element={<TemplateCenter />} />
        <Route path="review" element={<Review />} />
        <Route path="guide" element={<ShootingGuide />} />
        <Route path="admin" element={<Admin />} />
        <Route path="settings" element={<SettingsPage />} />
        <Route path="*" element={<Soon title="页面不存在" desc="检查一下地址，或者从左侧导航进入。" />} />
      </Route>
    </Routes>
  )
}
