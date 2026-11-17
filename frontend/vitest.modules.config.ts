import { defineConfig, mergeConfig } from 'vitest/config';

import viteConfig from './vite.config.ts';

// The installed-module lane. It drives a real Chromium over the app, in front
// of a core that has the two proof extensions and the refused fixture on it.
// `scripts/check-example-spools.sh` is what starts that core and what runs
// this, because installing a module is its job and drawing one is this app's.
//
// A config of its own, like the measure lane: it needs a server this checkout
// does not start, so `pnpm run check` cannot run it.
export default mergeConfig(
  viteConfig,
  defineConfig({
    test: {
      name: 'modules',
      include: ['test/modules/**/*.test.ts'],
      environment: 'node',
      globalSetup: ['./test/modules/global-setup.ts'],
      fileParallelism: false,
      testTimeout: 180_000,
      hookTimeout: 180_000,
    },
  }),
);
