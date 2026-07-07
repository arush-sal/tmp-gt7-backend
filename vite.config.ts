import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
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
