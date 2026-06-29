import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  test: {
    // `globals: true` exposes describe/test/expect without importing them in
    // every file — matches the ergonomics you'd expect from Go's testing pkg,
    // where `t` is just there. We still import explicitly in examples for clarity.
    globals: true,
    // 'node' is enough for validate.ts/api.ts (pure logic + fetch). When we test
    // the React hook later we'll switch the hook's tests to a 'jsdom' environment.
    environment: 'node',
  },
})
