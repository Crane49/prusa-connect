// This package's own lint config. It is plain JavaScript with no Svelte and
// no application code, so it needs none of the three configs it publishes,
// only the core rules every one of them also carries.
import js from '@eslint/js';
import prettier from 'eslint-config-prettier';
import { defineConfig } from 'eslint/config';
import globals from 'globals';

export default defineConfig(
  {
    ignores: [
      'node_modules/',
      'pnpm-lock.yaml',
      // Deliberately bad fixtures the test suite lints on purpose.
      'test/fixtures/',
    ],
  },
  js.configs.recommended,
  prettier,
  {
    languageOptions: { globals: globals.node },
    rules: {
      eqeqeq: ['error', 'always', { null: 'ignore' }],
      'no-nested-ternary': 'error',
    },
  },
);
