import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  server: {
    port: 5173,
    strictPort: true,
    host: true,
    allowedHosts: ['host.docker.internal', 'localhost'],
  },
  build: {
    outDir: 'dist',
    assetsDir: 'assets',
  },
})
