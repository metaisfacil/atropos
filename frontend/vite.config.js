import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'
import { defineConfig as defineVitestConfig } from 'vitest/config'

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

export const testConfig = defineVitestConfig({
  test: {
    environment: 'jsdom',
    globals: false,      // or true if you prefer
    setupFiles: './src/setupTests.js', // if needed
    include: ['src/**/*.test.js', 'src/**/*.test.ts'],
  },
})