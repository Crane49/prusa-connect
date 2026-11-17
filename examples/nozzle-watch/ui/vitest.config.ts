import { svelte } from '@sveltejs/vite-plugin-svelte';
import { playwright } from '@vitest/browser-playwright';
import { defineConfig } from 'vitest/config';

// The tests do not read `vite.config.ts`: that file is the library build, and
// merging a `build.lib` into a test run would ask Vitest to bundle the module
// rather than run it.
//
// Most tests here touch runes or the DOM, and this project has one runner, so
// every test file runs in a real Chromium (svelte-testing.md), `mark-changed`'s
// plain functions included. There is no stylesheet: the module ships none,
// because the app owns the theme and this bundle draws with its utilities. So
// a test renders unstyled on purpose and asserts behaviour and roles.
export default defineConfig({
  plugins: [svelte()],
  test: {
    name: 'browser',
    include: ['src/**/*.test.ts'],
    browser: {
      enabled: true,
      headless: true,
      provider: playwright(),
      instances: [{ browser: 'chromium' }],
      screenshotFailures: false,
    },
  },
});
