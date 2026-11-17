// A fixture for the tsconfig test: `tsc -p` against a tsconfig that extends
// ours must exit 0 on this file, proving the base config is loadable, its
// compiler options do not conflict with each other, and a consumer's
// eslint.config.js reads the three configs typed rather than as `any`.

import type { Linter } from 'eslint';

import { applicationConfig } from '@prusa/frontend-standards/eslint/application';
import { libraryConfig } from '@prusa/frontend-standards/eslint/library';
import { moduleConfig } from '@prusa/frontend-standards/eslint/module';
import prettierConfig from '@prusa/frontend-standards/prettier';

export const first = (values: readonly string[]): string | undefined =>
  values[0];

const options = { tsconfigRootDir: '.', svelteConfig: {} };

export const configs: Linter.Config[][] = [
  applicationConfig(options),
  libraryConfig(options),
  moduleConfig(options),
];

// A consumer's own .prettierrc.js spreads this default export, so it must
// resolve typed rather than as `any` (prettier.d.ts).
export const prettierTabWidth: number | undefined = prettierConfig.tabWidth;
