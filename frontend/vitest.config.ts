import { playwright } from '@vitest/browser-playwright';
import { defineConfig, mergeConfig } from 'vitest/config';

import viteConfig from './vite.config.ts';
import { chromiumGcArgs } from '../packages/svelte/src/test/chromium-gc.ts';

// Two projects, on the component library's model. A component or rune test
// runs in a real Chromium through Playwright (svelte-testing.md), and the
// `.svelte.` infix is what routes it there. The gates that read the source
// tree from disk run under Node.
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
            include: [
              'src/**/*.test.ts',
              '.storybook/**/*.test.ts',
              'test/story/**/*.test.ts',
            ],
            exclude: ['src/**/*.svelte.test.ts'],
            environment: 'node',
          },
        },
      ],
    },
  }),
);
