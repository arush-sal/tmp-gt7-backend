import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

// RTPS backend address for the dev proxy - set by `make up RTPS=...` via GT7_API_ADDR.
// Expects a bare host:port (e.g. 192.168.1.50:8080) or an origin URL (no path); defaults
// to localhost:8080. `process.env` is read at config load time (Node), not in the browser.
const rtps = process.env.GT7_API_ADDR?.trim() || 'localhost:8080'
const apiTarget = /^https?:\/\//i.test(rtps) ? rtps : `http://${rtps}`

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      // Forward the live API to the backend so the browser EventSource is same-origin in dev.
      // Dev-only: in production, serve the app same-origin as the API or route /v1/* to it at the edge.
      "/v1": { target: apiTarget, changeOrigin: true },
    },
  },
  test: {
    // `globals: true` exposes describe/test/expect without importing them in
    // every file — matches the ergonomics you'd expect from Go's testing pkg,
    // where `t` is just there. We still import explicitly in examples for clarity.
    globals: true,
    // 'node' is the default for pure-logic libs (validateLive, format, liveView).
    // The React hook tests opt into 'jsdom' per-file via a `// @vitest-environment jsdom` docblock.
    environment: 'node',
    passWithNoTests: true,   // suites are added task-by-task; empty run must not fail the gate
  },
})
