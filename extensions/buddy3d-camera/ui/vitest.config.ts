import { defineConfig } from 'vitest/config';

// The tests here are of plain functions, so they run in Node. A test that
// touches runes or the DOM would need the browser lane the core's modules use.
export default defineConfig({
  test: { include: ['src/**/*.test.ts'], environment: 'node' },
});
