import { defineConfig, mergeConfig } from 'vitest/config';

import viteConfig from '../../vite.config.ts';

// The journey lane. It drives a real Chromium over the application, and the
// application talks to a core that is really running: no fixture answers
// anything. `scripts/check-journey.sh` starts that core and runs this.
//
// A config of its own, because `pnpm run check` cannot run it. It needs a
// server this package does not start, and it reads the address, the account
// and the file to print out of the environment.
export default mergeConfig(
  viteConfig,
  defineConfig({
    test: {
      name: 'journey',
      include: ['test/journey/**/*.test.ts'],
      environment: 'node',
      globalSetup: ['./test/journey/global-setup.ts'],
      // One browser at a time. The journey prints on a printer, and a second
      // run beside it would find that printer busy.
      fileParallelism: false,
      testTimeout: 180_000,
      hookTimeout: 180_000,
    },
  }),
);
