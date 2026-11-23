// The bare specifiers the app's import map answers, and a module's build
// therefore leaves external (ADR-20260911-4p6 rules 27 and 28).
//
// Moved from pull request 185, packages/core/src/plugins/import-map.ts, where
// the same list is the `SHARED` constant. Two entries are added on earlier research
// recommendation 20: the library and the SDK, so a bundle draws with the
// design system and calls the core without carrying either.
//
// The list has one home because two sides read it. The app emits a chunk per
// entry and writes the map; `moduleBuild()` marks each one external. A
// specifier on one side and not the other reaches the browser unresolved and
// fails the module's whole bundle.

/**
 * What a compiled Svelte component imports at run time, plus the public entry
 * a component reaches for `onMount`, `mount`, `tick` and the context
 * functions.
 *
 * Svelte 5.57 emits `svelte/internal/disclose-version` and
 * `svelte/internal/client` from every component, and
 * `svelte/internal/flags/legacy` from a component compiled outside runes mode.
 * `moduleBuild()` compiles in runes mode, so the third never appears.
 *
 * Any other `svelte/*` subpath, such as `svelte/transition`, is a specifier
 * the author writes rather than one the compiler emits. It stays out of the
 * map, so a bundle that uses one carries its own copy. That copy holds no
 * runtime state of its own: it imports `svelte/internal/client`, which the map
 * answers, so the page still has one runtime.
 */
const SVELTE = [
  'svelte',
  'svelte/internal/client',
  'svelte/internal/disclose-version',
] as const;

/**
 * The SDK, and the library with each subpath of its `exports` map that a
 * browser loads as a module, in the order that map writes them.
 *
 * Some of the map's entries are out, and each is out for a reason:
 *
 * - `./app.css` and `./tokens.css` are stylesheets. An import map resolves
 *   module specifiers, and a stylesheet is not a module. The app loads the
 *   theme once, and a module's markup reads it.
 * - `./build` is the tool subpath of ADR-20260914-244 rule 5. It runs in Node inside
 *   a module's own build and never reaches a browser.
 * - `./dashboard` is the application's dashboard screen, a chunk it loads
 *   when a dashboard is drawn. A module's card reads the host it was given
 *   and never this screen (ADR-20261008-sww rule 21).
 *
 * A subpath the package publishes and this list leaves out still resolves,
 * through the package rather than through the map, so nothing refuses it and
 * `moduleBuild()` compiles that whole tier into every bundle that imports it.
 * `specifiers.test.ts` reads the `exports` map and holds this list against it,
 * naming each omission, so a subpath the package gains is either added here or
 * given a written reason to stay out.
 */
const PRUSA = [
  '@prusa/connect',
  '@prusa/connect-ui',
  '@prusa/connect-ui/i18n',
  '@prusa/connect-ui/domain',
  '@prusa/connect-ui/data',
  '@prusa/connect-ui/slots',
  '@prusa/connect-ui/slicer',
  '@prusa/connect-ui/primitives',
  '@prusa/connect-ui/router',
  '@prusa/connect-ui/shell',
] as const;

/** Every key of the app's import map, in the order the map is written. */
export const SHARED_SPECIFIERS: readonly string[] = [...SVELTE, ...PRUSA];

/**
 * The chunk name the app emits a specifier under. Rollup adds the content
 * hash, so the address changes when the code does and a browser cache cannot
 * serve a stale runtime.
 */
export const chunkNameOf = (specifier: string): string =>
  specifier.replace(/^@/, '').replace(/[/.]/g, '-');
