import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      // 开发时把 API 代理到 Go 复盘服务
      '/v1': 'http://localhost:8080',
    },
  },
})
