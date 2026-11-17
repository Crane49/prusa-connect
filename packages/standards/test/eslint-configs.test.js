// Proves each of the three exported configs loads and actually lints: a
// config that only "imports without throwing" could still be an empty array
// that catches nothing. Type-aware rules are exercised here through a real
// tsconfig read by `projectService`; this does not prove every rule in
// strictTypeChecked fires, only that the wiring (parserOptions,
// tsconfigRootDir, the svelte parser, the plugin) is the wiring a real
// consumer gets.

import assert from 'node:assert/strict';
import { fileURLToPath } from 'node:url';
import { describe, it } from 'node:test';

import { ESLint } from 'eslint';

import { applicationConfig } from '../eslint/application.js';
import { libraryConfig } from '../eslint/library.js';
import { moduleConfig } from '../eslint/module.js';

const fixtureDir = fileURLToPath(new URL('fixtures/project/', import.meta.url));

/** @type {[string, (options: { tsconfigRootDir: string, svelteConfig: unknown }) => import('eslint').Linter.Config[]][]} */
const configs = [
  ['library', libraryConfig],
  ['application', applicationConfig],
  ['module', moduleConfig],
];

for (const [name, factory] of configs) {
  describe(`the ${name} config`, () => {
    it('loads as a non-empty flat config', () => {
      const svelteConfig = { compilerOptions: { runes: true } };
      const config = factory({ tsconfigRootDir: fixtureDir, svelteConfig });

      assert.ok(Array.isArray(config));
      assert.ok(config.length > 0);
    });

    it('refuses console.log through no-console', async () => {
      const svelteConfigModule = await import(`${fixtureDir}svelte.config.js`);
      const eslint = new ESLint({
        cwd: fixtureDir,
        overrideConfigFile: true,
        overrideConfig: factory({
          tsconfigRootDir: fixtureDir,
          svelteConfig: svelteConfigModule.default,
        }),
      });

      const [result] = await eslint.lintFiles(['console.js']);
      const ruleIds = result.messages.map((message) => message.ruleId);

      assert.ok(ruleIds.includes('no-console'), ruleIds.join(', '));
    });

    it('refuses English markup text through no-literal-text', async () => {
      const svelteConfigModule = await import(`${fixtureDir}svelte.config.js`);
      const eslint = new ESLint({
        cwd: fixtureDir,
        overrideConfigFile: true,
        overrideConfig: factory({
          tsconfigRootDir: fixtureDir,
          svelteConfig: svelteConfigModule.default,
        }),
      });

      const [result] = await eslint.lintFiles(['literal.svelte']);
      const ruleIds = result.messages.map((message) => message.ruleId);

      assert.ok(
        ruleIds.includes('frontend-standards/no-literal-text'),
        ruleIds.join(', '),
      );
    });

    it('passes a component with no console call and no literal text', async () => {
      const svelteConfigModule = await import(`${fixtureDir}svelte.config.js`);
      const eslint = new ESLint({
        cwd: fixtureDir,
        overrideConfigFile: true,
        overrideConfig: factory({
          tsconfigRootDir: fixtureDir,
          svelteConfig: svelteConfigModule.default,
        }),
      });

      const [result] = await eslint.lintFiles(['clean.svelte']);
      const ruleIds = result.messages.map((message) => message.ruleId);

      assert.ok(
        !ruleIds.includes('no-console') &&
          !ruleIds.includes('frontend-standards/no-literal-text'),
        ruleIds.join(', '),
      );
    });

    /** @param {string} file */
    const ruleIdsOf = async (file) => {
      const svelteConfigModule = await import(`${fixtureDir}svelte.config.js`);
      const eslint = new ESLint({
        cwd: fixtureDir,
        overrideConfigFile: true,
        overrideConfig: factory({
          tsconfigRootDir: fixtureDir,
          svelteConfig: svelteConfigModule.default,
        }),
      });
      const [result] = await eslint.lintFiles([file]);
      return result.messages.map((message) => message.ruleId);
    };

    it('refuses a raw <button> through no-restricted-html-elements', async () => {
      const ruleIds = await ruleIdsOf('raw-button.svelte');
      assert.ok(
        ruleIds.includes('svelte/no-restricted-html-elements'),
        ruleIds.join(', '),
      );
    });

    it('passes a primitive in place of the raw element', async () => {
      const ruleIds = await ruleIdsOf('primitive-button.svelte');
      assert.ok(
        !ruleIds.includes('svelte/no-restricted-html-elements'),
        ruleIds.join(', '),
      );
    });

    it('passes a raw element under a disable with a reason', async () => {
      const ruleIds = await ruleIdsOf('disabled-with-reason.svelte');
      assert.deepEqual(ruleIds, []);
    });

    it('refuses a disable that excuses nothing', async () => {
      const ruleIds = await ruleIdsOf('unused-disable.svelte');
      assert.ok(
        ruleIds.includes('svelte/comment-directive'),
        ruleIds.join(', '),
      );
    });

    it('refuses a hand-drawn spinner through no-restricted-syntax', async () => {
      const ruleIds = await ruleIdsOf('spinner-class.svelte');
      assert.ok(ruleIds.includes('no-restricted-syntax'), ruleIds.join(', '));
    });

    it('refuses role="alert" through no-restricted-syntax', async () => {
      const ruleIds = await ruleIdsOf('role-alert.svelte');
      assert.ok(ruleIds.includes('no-restricted-syntax'), ruleIds.join(', '));
    });
  });
}
