import { NavLink } from 'react-router-dom'

// 商品库顶上的切换：按商品看 / 看全部图片和视频（原来的素材库）
export function LibrarySwitch() {
  return (
    <div className="segmented">
      <NavLink to="/products" className={({ isActive }) => isActive ? 'active' : ''}>按商品</NavLink>
      <NavLink to="/assets" className={({ isActive }) => isActive ? 'active' : ''}>全部图片和视频</NavLink>
    </div>
  )
}
