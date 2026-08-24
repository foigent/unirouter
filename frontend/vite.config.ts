import { readFileSync } from 'node:fs'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import WarningRatchetReporter from './scripts/vitest-warning-reporter'

const packageVersion = JSON.parse(
  readFileSync(new URL('./package.json', import.meta.url), 'utf8'),
).version as string

/// <reference types="vitest" />
export default defineConfig({
  plugins: [react()],
  define: {
    __UNIROUTER_VERSION__: JSON.stringify(
      String(process.env.VITE_APP_VERSION || packageVersion).trim(),
    ),
  },
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
    reporters: ['default', new WarningRatchetReporter()],
    css: false,
    testTimeout: 10_000,
  },
  server: {
    port: 3000,
    proxy: {
      '/v1': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/health': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
})
