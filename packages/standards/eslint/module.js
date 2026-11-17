// The flat config for a module plugin: the browser half of a module,
// bundled by `moduleBuild()` and reaching the core through the host object
// alone (ADR-20260911-4p6 rules 18 and 20).
//
//   import { moduleConfig } from '@prusa/frontend-standards/eslint/module';
//   import svelteConfig from './svelte.config.js';
//
//   export default moduleConfig({
//     tsconfigRootDir: import.meta.dirname,
//     svelteConfig,
//   });

import { defineConfig } from 'eslint/config';

import {
  baseIgnores,
  baseRuleSets,
  commonRules,
  languageOptions,
  noLiteralTextRule,
  rawControlRule,
  svelteFileParser,
  svelteRenderTagRule,
  testFileOverride,
} from './shared.js';

// ADR-20260911-4p6 rules 18 and 20: a module's bundle reaches the core through the
// host's SDK and holds no token. A `fetch` here would be a module opening a
// door the host did not authorize.
const TRANSPORT_MESSAGE =
  'A module reaches the core through host.client, the SDK in session mode (ADR-20260911-4p6 rules 18 and 20).';

const ARBITRARY_MESSAGE =
  'A Tailwind arbitrary value. Add the value to the theme in the component library and use its name (frontend-css.md).';

/**
 * @param {{ tsconfigRootDir: string, svelteConfig: unknown }} options
 * @returns {import('eslint').Linter.Config[]}
 */
export const moduleConfig = ({ tsconfigRootDir, svelteConfig }) =>
  defineConfig(
    { ignores: baseIgnores },
    ...baseRuleSets,
    languageOptions(tsconfigRootDir),
    commonRules({
      transportMessage: TRANSPORT_MESSAGE,
      arbitraryMessage: ARBITRARY_MESSAGE,
    }),
    svelteFileParser(svelteConfig),
    svelteRenderTagRule(),
    noLiteralTextRule(),
    rawControlRule(),
    testFileOverride(),
  );

export default moduleConfig;
