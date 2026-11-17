import { defineConfig, mergeConfig } from 'vitest/config';

import viteConfig from './vite.config.ts';

// The measure lane (earlier research task 1). It drives a real Chromium over the
// built app on a server that answers every API call from a file, so it needs
// no core and no network. It is a config of its own rather than a third
// project in `vitest.config.ts`, because it builds the app first and
// `pnpm run check` is meant to stay quick. `make frontend-check` runs both.
export default mergeConfig(
  viteConfig,
  defineConfig({
    test: {
      name: 'ux',
      include: ['test/ux/**/*.test.ts'],
      environment: 'node',
      globalSetup: ['./test/ux/global-setup.ts'],
      // Every file and every test in it at once. What is read off a page is
      // its geometry and its colours, which a busy machine paints the same,
      // and each test waits for its own screen to settle rather than for a
      // fixed time a busy machine could overrun. The one timed measure, the
      // camera cadence, allows ten times the tick either way.
      fileParallelism: true,
      sequence: { concurrent: true },
      maxConcurrency: 6,
      testTimeout: 180_000,
      hookTimeout: 180_000,
    },
  }),
);
