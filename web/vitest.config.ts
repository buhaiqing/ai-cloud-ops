import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    environment: 'happy-dom',
    globals: true,
    setupFiles: ['./tests/setup.ts'],
    // Only the test files owned by this delivery. Other subagent suites
    // (e.g. rules.test.tsx) live here too but reference components that
    // aren't part of M2-2 / M2-9; they get picked up when their owners land.
    include: [
      'tests/resources.test.tsx',
      'tests/stats.test.tsx',
      'tests/rules.test.tsx',
      // M2-5 + M2-8 (auth + ws client)
      'lib/auth.test.ts',
      'lib/ws.test.ts',
      'lib/api.test.ts',
      'tests/login.test.tsx',
    ],
  },
  // Next.js' tsconfig.json uses "jsx": "preserve", which esbuild would respect,
  // leaving JSX untransformed in tests. Override it just for vitest so the
  // automatic JSX transform runs (no React import boilerplate in tests).
  esbuild: {
    jsx: 'automatic',
    jsxImportSource: 'react',
  },
  resolve: {
    alias: {
      '@': new URL('./', import.meta.url).pathname,
    },
  },
});
