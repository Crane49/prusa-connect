// What the `svelte` export condition points at: one compiled `.js` for every
// `.ts` and `.svelte.ts` under src/, every `.svelte` file copied as it was
// written, and the stylesheets.
//
// A Svelte library ships its components as `.svelte` source, because the
// consumer's own Svelte compiler is the one that compiles them. What it must
// not ship is TypeScript. Vite's dependency optimizer hands a `.svelte.ts`
// file to the Svelte module compiler, which reads JavaScript and stops at the
// first type annotation, and a consumer then needs
// `optimizeDeps: { exclude: ['@prusa/connect-ui'] }` before its dev
// server will start. `.svelte.js` is the name Svelte reads a rune module
// under, so the extension is the only thing that changes.
//
// This is what `svelte-package` does, without the meta-framework dependency
// ADR-20260911-4p6 rule 2 refuses. TypeScript's own transpiler writes the JavaScript,
// so no tool is added and nothing is type-checked twice: annotations are
// removed, and `$state`, `$derived` and `$effect` are calls it leaves alone.
// svelte/compiler then compiles every emitted rune module, which is the check
// that catches the fault this replaces.
import { cp, mkdir, readdir, readFile, rm, writeFile } from 'node:fs/promises';
import { dirname, join, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

import { compileModule } from 'svelte/compiler';
import ts from 'typescript';

const ROOT = fileURLToPath(new URL('..', import.meta.url));
const SRC = join(ROOT, 'src');
const DIST = join(ROOT, 'dist');

/**
 * What never ships: the tests, the components they render, and the stories
 * Storybook draws. A story sits beside its component so it is easy to find,
 * and it is a development tool, so it stays out of the published package.
 *
 * @param {string} name
 * @returns {boolean}
 */
const shipped = (name) =>
  !name.endsWith('.test.ts') &&
  !name.endsWith('.stories.svelte') &&
  !name.split(sep).includes('test');

/**
 * Every shipped file under src/, by its path relative to src/.
 *
 * @param {string} at
 * @returns {Promise<string[]>}
 */
const sources = async (at = '') => {
  const entries = await readdir(join(SRC, at), { withFileTypes: true });
  /** @type {string[]} */
  const found = [];

  for (const entry of entries) {
    const name = at === '' ? entry.name : join(at, entry.name);
    if (!shipped(name)) continue;
    found.push(...(entry.isDirectory() ? await sources(name) : [name]));
  }

  return found;
};

// The output Node and every bundler reads. `verbatimModuleSyntax` keeps the
// import list exactly as it was written, which a file compiled on its own
// needs: nothing here knows whether a name is a type.
const OPTIONS = {
  target: ts.ScriptTarget.ES2022,
  module: ts.ModuleKind.ESNext,
  isolatedModules: true,
  verbatimModuleSyntax: true,
};

/**
 * @param {string} source
 * @param {string} fileName
 * @returns {string}
 */
const transpile = (source, fileName) => {
  const { outputText, diagnostics } = ts.transpileModule(source, {
    fileName,
    compilerOptions: OPTIONS,
    reportDiagnostics: true,
  });

  const failures = diagnostics ?? [];
  if (failures.length > 0) {
    const said = failures
      .map((one) => ts.flattenDiagnosticMessageText(one.messageText, ' '))
      .join('; ');
    throw new Error(`${fileName} did not transpile: ${said}`);
  }

  return outputText;
};

// The retries are for Windows, where a virus scanner or the file indexer can
// still hold a handle on a file this has just unlinked, and the directory
// above it then fails to be removed with ENOTEMPTY.
await rm(DIST, {
  recursive: true,
  force: true,
  maxRetries: 10,
  retryDelay: 100,
});

/** @type {string[]} */
const written = [];

for (const name of await sources()) {
  const from = join(SRC, name);
  const to = join(
    DIST,
    name.endsWith('.ts') ? `${name.slice(0, -3)}.js` : name,
  );

  await mkdir(dirname(to), { recursive: true });
  if (name.endsWith('.ts')) {
    await writeFile(to, transpile(await readFile(from, 'utf8'), from));
  } else {
    await cp(from, to);
  }
  written.push(to);
}

// The rune modules, through the compiler a consumer's build will use. A file
// this fails on is one that would have failed in a consumer's dev server.
for (const file of written.filter((name) => name.endsWith('.svelte.js'))) {
  compileModule(await readFile(file, 'utf8'), {
    filename: file,
    generate: 'client',
  });
}
