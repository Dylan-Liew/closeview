import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const projectRoot = path.dirname(fileURLToPath(import.meta.url))

export default defineConfig({
  root: path.resolve(projectRoot, 'internal/server/websrc'),
  plugins: [react()],
  resolve: {
    alias: { '@': path.resolve(projectRoot, 'internal/server/websrc/src') },
  },
  build: {
    outDir: path.resolve(projectRoot, 'internal/server/web'),
    emptyOutDir: true,
  },
  server: {
    proxy: { '/api': 'http://127.0.0.1:3434' },
  },
})
