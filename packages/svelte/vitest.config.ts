import { playwright } from '@vitest/browser-playwright';
import { defineConfig, mergeConfig } from 'vitest/config';

import viteConfig from './vite.config.ts';
import { chromiumGcArgs } from './src/test/chromium-gc.ts';

// Two projects. A component test runs in a real Chromium through Playwright
// (svelte-testing.md), and the `.svelte.` infix is what routes it there. The
// token tests read the stylesheet from disk and run the Tailwind compiler, so
// they run under Node.
export default mergeConfig(
  viteConfig,
  defineConfig({
    test: {
      projects: [
        {
          extends: true,
          test: {
            name: 'browser',
            include: ['src/**/*.svelte.test.ts'],
            setupFiles: ['./src/test/setup.ts'],
            // A retried assertion waits five seconds, not the one second the
            // browser default gives. On a loaded runner a panel closes late
            // and the default reported it as still open.
            expect: { poll: { timeout: 5000 } },
            browser: {
              enabled: true,
              headless: true,
              provider: playwright({ launchOptions: { args: chromiumGcArgs } }),
              instances: [{ browser: 'chromium' }],
              screenshotFailures: false,
            },
          },
        },
        {
          extends: true,
          test: {
            name: 'node',
            include: ['src/**/*.test.ts', 'build/**/*.test.ts'],
            exclude: ['src/**/*.svelte.test.ts'],
            environment: 'node',
          },
        },
      ],
    },
  }),
);
