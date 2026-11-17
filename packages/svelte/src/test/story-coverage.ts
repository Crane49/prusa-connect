// The component half of the Storybook rule (.agents/rules/storybook.md): every
// component a barrel exports is imported by some story file. A story file
// covers a component when it imports the `.svelte` file itself, the barrel of
// the component's own folder, or a story-only wrapper that imports either. Imports through a package name do not count,
// so a library component needs a story in the library.
import { existsSync, readdirSync, readFileSync } from 'node:fs';
import { dirname, join, relative, resolve } from 'node:path';

const walk = (dir: string): string[] =>
  readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name);
    if (entry.isDirectory())
      return entry.name === 'node_modules' ? [] : walk(path);
    return [path];
  });

const SPECIFIER = /(?:from|import)\s+'(\.[^']+)'/g;

const RULE = '.agents/rules/storybook.md';

/** Every `.svelte` file a barrel `index.ts` under `dirs` exports. */
const exported = (dirs: string[]): string[] =>
  dirs
    .flatMap(walk)
    .filter((path) => path.endsWith('/index.ts'))
    .flatMap((barrel) =>
      [...readFileSync(barrel, 'utf8').matchAll(SPECIFIER)]
        .map((match) => resolve(dirname(barrel), match[1] ?? ''))
        // `./store.svelte` names `store.svelte.ts`, which is no component.
        .filter((path) => path.endsWith('.svelte') && existsSync(path)),
    );

const imports = (file: string): string[] =>
  [...readFileSync(file, 'utf8').matchAll(SPECIFIER)].map((match) =>
    resolve(dirname(file), match[1] ?? '').replace(/\/index(\.ts)?$/, ''),
  );

/**
 * The `.svelte` files and folders the story files under `dirs` import, and
 * what a story-only wrapper they import imports in turn. The walk stops at a
 * barrel component, which needs a story of its own.
 */
const storied = (dirs: string[], components: Set<string>): Set<string> => {
  const seen = new Set<string>();
  const queue = dirs
    .flatMap(walk)
    .filter((path) => path.endsWith('.stories.svelte'))
    .flatMap(imports);
  for (let path = queue.pop(); path !== undefined; path = queue.pop()) {
    if (seen.has(path)) continue;
    seen.add(path);
    if (path.endsWith('.svelte') && !components.has(path) && existsSync(path))
      queue.push(...imports(path));
  }
  return seen;
};

/**
 * The barrel components under `components` that no story file under `stories`
 * imports, as paths relative to `root`.
 */
export const unstoried = (
  root: string,
  components: string[],
  stories: string[],
): string[] => {
  const found = new Set(exported(components.map((dir) => join(root, dir))));
  const covered = storied(
    stories.map((dir) => join(root, dir)),
    found,
  );
  return [...found]
    .filter((path) => !covered.has(path) && !covered.has(dirname(path)))
    .map((path) => relative(root, path))
    .sort();
};

/**
 * The paths a baseline file lists: one per line, a path, a space and the
 * reason. Lines starting with `#` are comments.
 */
export const baseline = (file: string): string[] =>
  readFileSync(file, 'utf8')
    .split('\n')
    .filter((line) => line.trim() !== '' && !line.startsWith('#'))
    .map((line) => line.split(' ')[0] ?? '');

/** What the gate says for a component without a story. */
export const missingMessage = (path: string): string =>
  `${path}: no story file imports it. Add ${path.replace(/\.svelte$/, '.stories.svelte')} (see ${RULE})`;

/** What the gate says for a baseline line whose component now has a story. */
export const staleMessage = (path: string, file: string): string =>
  `${path} has a story now. Delete its line from ${file} (see ${RULE})`;
