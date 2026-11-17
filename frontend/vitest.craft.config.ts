import { defineConfig, mergeConfig } from 'vitest/config';

import viteConfig from './vite.config.ts';

// The craft lane (earlier research task 2). It counts what a rendered route is made
// of: radii, text sizes, greys, borders, primary buttons, icons, punctuation
// and figures. It drives the same Chromium over the same built app as the UX
// lane, and reuses that lane's server and fixtures through
// `test/ux/global-setup.ts`.
//
// A config of its own rather than a third project in `vitest.config.ts`, for
// the reason the UX lane gives: it builds the app first and `pnpm run
// check` is meant to stay quick. `make frontend-check` runs all three.
export default mergeConfig(
  viteConfig,
  defineConfig({
    test: {
      name: 'craft',
      include: ['test/craft/**/*.test.ts'],
      environment: 'node',
      globalSetup: ['./test/ux/global-setup.ts'],
      // Every test at once. A count is read off what the page painted, which
      // a busy machine paints the same, and each test waits for its own
      // screen to settle rather than for a fixed time it could overrun.
      fileParallelism: true,
      sequence: { concurrent: true },
      maxConcurrency: 6,
      testTimeout: 180_000,
      hookTimeout: 180_000,
    },
  }),
);
