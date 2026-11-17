import { applicationConfig } from '@prusa/frontend-standards/eslint/application';

import svelteConfig from './svelte.config.js';

export default [
  ...applicationConfig({
    tsconfigRootDir: import.meta.dirname,
    svelteConfig,
  }),
  { ignores: ['storybook-static/'] },
  // The logger is the one place that writes to the console
  // (frontend-logging.md).
  { files: ['src/lib/log/**'], rules: { 'no-console': 'off' } },
];
