import { svelte } from '@sveltejs/vite-plugin-svelte';
import { playwright } from '@vitest/browser-playwright';
import { defineConfig } from 'vitest/config';

// Every test runs in a real Chromium. The zone is pinned because the heatmap
// buckets by local hour and a DST day must read the same on every machine.
export default defineConfig({
  plugins: [svelte()],
  test: {
    name: 'browser',
    include: ['src/**/*.test.ts'],
    browser: {
      enabled: true,
      headless: true,
      provider: playwright({ contextOptions: { timezoneId: 'Europe/Prague' } }),
      instances: [{ browser: 'chromium' }],
      screenshotFailures: false,
    },
  },
});
