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
      // The browser only ever talks to the authentication gateway
      // (cmd/gateway, `task run:gateway`): it completes the GitHub login, owns
      // the HttpOnly session cookie and signs the internal credentials the
      // cloud verifies. Proxying keeps everything same-origin with this dev
      // server, which is what the gateway's cookie and Origin checks require
      // (its `public.base_url` is this origin).
      '/auth': 'http://localhost:8081',
      '/api': 'http://localhost:8081',
      '/healthz': 'http://localhost:8081',
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
