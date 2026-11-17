import { defineConfig, mergeConfig } from 'vitest/config';

import viteConfig from '../../vite.config.ts';

// The sign-in lane against real OpenID Connect providers. A real Chromium
// walks the first-run wizard of a core that is really running, the core
// restarts on the answers, and people sign in at a provider in a container.
// `scripts/check-oidc-providers.sh` starts the provider and the core, once per
// provider, and runs this.
//
// A config of its own, because `pnpm run check` cannot run it: it needs a core
// and a provider this package does not start, and reads both out of the
// environment.
export default mergeConfig(
  viteConfig,
  defineConfig({
    test: {
      name: 'oidc',
      include: ['test/oidc/**/*.test.ts'],
      environment: 'node',
      // One browser, and the cases run in order: each one stands on the
      // server the one before it left.
      fileParallelism: false,
      sequence: { concurrent: false },
      testTimeout: 180_000,
      hookTimeout: 180_000,
    },
  }),
);
