import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

import { describe, expect, it } from 'vitest';

import { SHARED_SPECIFIERS } from './specifiers';

const PACKAGE = fileURLToPath(new URL('../package.json', import.meta.url));

/** The name the `exports` map's keys are subpaths of. */
const LIBRARY = '@prusa/connect-ui';

/**
 * The `exports` keys the import map deliberately does not answer, each with
 * the reason `specifiers.ts` records. A key here and not in the map is an
 * exception somebody wrote down. A key in neither fails, because an exception
 * nobody wrote down is a tier every module that imports it compiles into its
 * own bundle.
 */
const NOT_A_MODULE_SPECIFIER: Record<string, string> = {
  './app.css': 'a stylesheet, and an import map resolves modules',
  './tokens.css': 'a stylesheet, and an import map resolves modules',
  './build': 'a tool subpath, run in Node inside a module build',
  './dashboard': 'the application dashboard screen, which a card never imports',
  './setup-wizard': 'the first-run wizard, which a module never imports',
};

/**
 * Every subpath the package publishes, by name and in the order its `exports`
 * map writes them.
 *
 * The list above says which of those the import map answers. This one says
 * which subpaths exist at all, so a subpath added to the package or taken out
 * of it fails here and is read by somebody, rather than being carried into a
 * release by a gate that agreed with whatever the manifest happened to say.
 */
const PUBLISHED = [
  '.',
  './app.css',
  './tokens.css',
  './build',
  './i18n',
  './domain',
  './data',
  './slots',
  './slicer',
  './primitives',
  './router',
  './shell',
  './dashboard',
  './setup-wizard',
];

/**
 * Every key of the package's own `exports` map, in the order it writes them.
 * The manifest crosses into the test as `unknown` and is narrowed here, the
 * way `module.test.ts` narrows a built chunk (typescript.md).
 */
const exportKeys = (): string[] => {
  const manifest: unknown = JSON.parse(readFileSync(PACKAGE, 'utf8'));
  if (typeof manifest !== 'object' || manifest === null) {
    throw new Error(`${PACKAGE} is not an object`);
  }
  const { exports } = manifest as { exports?: unknown };
  if (typeof exports !== 'object' || exports === null) {
    throw new Error(`${PACKAGE} has no exports map`);
  }

  return Object.keys(exports);
};

/** `.` is the package itself, and `./x` is the specifier a consumer writes. */
const specifierOf = (key: string): string =>
  key === '.' ? LIBRARY : `${LIBRARY}/${key.slice('./'.length)}`;

/** What the list says about this package, with the SDK's own entry dropped. */
const libraryHalf = (): string[] =>
  SHARED_SPECIFIERS.filter(
    (one) => one === LIBRARY || one.startsWith(`${LIBRARY}/`),
  );

describe('SHARED_SPECIFIERS against the package exports map', () => {
  it('publishes the subpaths named here and no others', () => {
    expect(exportKeys()).toEqual(PUBLISHED);
  });

  it('answers every published subpath a browser loads as a module', () => {
    const answered = exportKeys()
      .filter((key) => !(key in NOT_A_MODULE_SPECIFIER))
      .map(specifierOf);

    expect(libraryHalf()).toEqual(answered);
  });

  it('names only omissions the package still publishes', () => {
    const published = new Set(exportKeys());
    const stale = Object.keys(NOT_A_MODULE_SPECIFIER).filter(
      (key) => !published.has(key),
    );

    expect(stale).toEqual([]);
  });

  it('leaves every named omission out of the map', () => {
    const wrongly = Object.keys(NOT_A_MODULE_SPECIFIER)
      .map(specifierOf)
      .filter((one) => libraryHalf().includes(one));

    expect(wrongly).toEqual([]);
  });
});
