import { svelte } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';
import { playwright } from '@vitest/browser-playwright';
import { defineConfig } from 'vitest/config';

// The tests do not read `vite.config.ts`: that file is the library build, and
// merging a `build.lib` into a test run would ask Vitest to bundle the module
// rather than run it.
//
// Every test here touches runes or the DOM, so every one runs in a real
// Chromium (svelte-testing.md).
//
// Tailwind is in this config and the setup file loads the theme, so a
// component renders in a test the way it renders on the host page.
export default defineConfig({
  plugins: [tailwindcss(), svelte()],
  test: {
    name: 'browser',
    include: ['src/**/*.svelte.test.ts'],
    setupFiles: ['./src/test/setup.ts'],
    browser: {
      enabled: true,
      headless: true,
      provider: playwright(),
      instances: [{ browser: 'chromium' }],
      screenshotFailures: false,
      // The page marks its parts with `data-part`, the same attribute the
      // library's own components carry, so a test scopes a query to one part
      // of the page without a second set of attributes put there for it.
      locators: { testIdAttribute: 'data-part' },
    },
  },
});
