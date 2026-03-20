import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [react()],
  server: {
    host: true,
    port: 5173,
    watch: {
      usePolling: true,
      interval: 200,
    },
    hmr: {
      protocol: 'ws',
      clientPort: 5173,
      overlay: true,
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: true,
  },
})