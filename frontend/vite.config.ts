import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// In dev the API is on :8080; nginx does the same /api prefix in the container.
export default defineConfig({
  plugins: [vue()],
  server: { proxy: { '/api': { target: 'http://localhost:8080', rewrite: (p) => p.replace(/^\/api/, '') } } },
  test: { setupFiles: ['./src/test-setup.ts'] },
})
