import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [vue(), tailwindcss()],
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
      '/ws': {
        target: 'ws://localhost:8080',
        ws: true,
      },
      '/mcp': 'http://localhost:8080',
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    exclude: ['e2e/**', 'node_modules/**'],
  },
})
