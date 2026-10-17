import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// 编译结果放到 ../web/dist，Go 用 embed 打进二进制。开发时 /api、/files、/render 转给本机后端。
const backend = process.env.XS_BACKEND || 'http://127.0.0.1:8080'
export default defineConfig({
  plugins: [react()],
  build: { outDir: '../web/dist', emptyOutDir: true, chunkSizeWarningLimit: 800 },
  server: {
    port: 5173,
    proxy: { '/api': backend, '/files': backend, '/render': backend }
  }
})
