import { defineConfig } from 'vitest/config';

// `npm test` runs the mocked unit tests; `npm run test:e2e` runs e2e/ against a real local stack.
const e2e = process.env.ABT_E2E === '1';
export default defineConfig({
  test: {
    environment: 'node',
    include: e2e ? ['e2e/**/*.test.ts'] : ['src/**/*.test.ts'],
    testTimeout: 30_000,
  },
});
