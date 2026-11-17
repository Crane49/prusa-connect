// Proves the rule the three exported configs wire in still fires, the same
// way it did as packages/svelte/eslint/no-literal-text.js. Nothing here
// exercised it before this file: neither package carried a dedicated test
// for it, only the real lint run over real components.

import { describe, it } from 'node:test';

import { RuleTester } from 'eslint';
import svelteEslintParser from 'svelte-eslint-parser';

import { noLiteralText } from '../eslint/rules/no-literal-text.js';

RuleTester.describe = describe;
RuleTester.it = it;

const ruleTester = new RuleTester({
  languageOptions: { parser: svelteEslintParser },
});

ruleTester.run('no-literal-text', noLiteralText, {
  valid: [
    { code: '<script>const value = 1;</script><p>{value}</p>' },
    { code: "<p>{t('fleet.empty')}</p>" },
    // A brand name and a unit stay in the allow list.
    { code: '<p>PRUSA</p>' },
    { code: '<button aria-label="{label}">x</button>' },
    // A single letter or a digit is never a word.
    { code: '<p>{count} x</p>' },
    // Script and style text is code, never a screen's text.
    { code: '<script>const s = "Hello";</script>' },
  ],
  invalid: [
    {
      code: '<p>Hello there</p>',
      errors: [{ messageId: 'literal' }],
    },
    {
      code: '<button title="Click me">x</button>',
      errors: [{ messageId: 'literal' }],
    },
    {
      code: '<img alt="A printer on a desk" />',
      errors: [{ messageId: 'literal' }],
    },
    {
      // The allow list is extendable, and a word outside it still fires.
      code: '<p>PLA spool</p>',
      options: [{ allow: ['PLA'] }],
      errors: [{ messageId: 'literal' }],
    },
  ],
});
