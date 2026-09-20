/// <reference types="vitest/config" />
import path from 'node:path'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': path.resolve(import.meta.dirname, './src'),
    },
  },
  server: {
    proxy: {
      // Development traffic goes through devgateway (cmd/devgateway), which
      // signs dual JWTs and proxies cloud verbatim; the browser never holds keys.
      '/api': 'http://localhost:8090',
      '/internal': 'http://localhost:8090',
      '/healthz': 'http://localhost:8090',
      '/devgateway': 'http://localhost:8090',
    },
  },
  test: {
    environment: 'jsdom',
    // Each test file builds its own jsdom environment; on many-core machines
    // the default worker count oversubscribes CPU and timing-sensitive
    // findBy assertions flake. GitHub runners expose 2-4 cores, so capping
    // keeps local gates deterministic without slowing CI down.
    maxWorkers: 4,
    setupFiles: ['src/test/setup.ts'],
    include: ['src/**/*.test.{ts,tsx}'],
    coverage: {
      provider: 'v8',
      include: ['src/**/*.{ts,tsx}'],
      // Generated code, the browser entry point and test scaffolding are not
      // units under test; everything else counts toward the thresholds.
      exclude: ['src/api/**', 'src/main.tsx', 'src/test/**', 'src/**/*.test.{ts,tsx}'],
      thresholds: { lines: 80, functions: 80, branches: 80, statements: 80 },
    },
  },
})
