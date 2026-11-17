// The one Prettier configuration every Prusa frontend formats against. A
// consumer points its own package.json at it and keeps no `.prettierrc` of
// its own:
//
//   "prettier": "@prusa/frontend-standards/prettier"
//
// `tailwindStylesheet` is not set here. prettier-plugin-tailwindcss resolves
// that path relative to the file that sets it, and this file lives inside
// node_modules, not inside a consumer's own tree, so a path written here
// could never reach a consumer's stylesheet. Left unset, the plugin falls
// back to Tailwind v4's own default stylesheet, which still sorts every
// standard utility correctly. The one thing it cannot see is a consumer's own
// `@utility` names, which then sort by the order they were written rather
// than Tailwind's canonical order.
//
// A consumer whose components use custom utilities adds one small file of its
// own, `.prettierrc.js`, spreading this config and naming its stylesheet:
//
//   import config from '@prusa/frontend-standards/prettier';
//   export default { ...config, tailwindStylesheet: './src/styles/tokens.css' };
//
// A bare `.prettierrc*` file is read instead of the `"prettier"` field in
// package.json, never merged with it, which is why the override spreads the
// shared config rather than naming the one key alone.

// The plugins are imported rather than named. Prettier resolves a plugin
// named as a string from the project it formats, and a consumer lists
// neither plugin: both are this package's own dependencies.
import * as sveltePlugin from 'prettier-plugin-svelte';
import * as tailwindPlugin from 'prettier-plugin-tailwindcss';

/** @type {import('prettier').Config} */
const config = {
  arrowParens: 'always',
  bracketSpacing: true,
  htmlWhitespaceSensitivity: 'css',
  insertPragma: false,
  bracketSameLine: false,
  printWidth: 80,
  proseWrap: 'preserve',
  quoteProps: 'as-needed',
  requirePragma: false,
  semi: true,
  singleQuote: true,
  tabWidth: 2,
  trailingComma: 'all',
  useTabs: false,
  endOfLine: 'auto',
  plugins: [sveltePlugin, tailwindPlugin],
  overrides: [{ files: '*.svelte', options: { parser: 'svelte' } }],
};

export default config;
