// The Vite config a module's bundle is built with (ADR-20260911-4p6 rule 28).
//
// Moved from pull request 185, plugins/nozzle-watch/vite.config.ts, where the
// same four choices are written out by hand in the plugin's own config. Here
// they are one function, because every module makes the same four and an
// author who gets one wrong finds out in the browser.
//
// What the four are:
//
//   1. one ES entry with a fixed file name, because the manifest's `ui.entry`
//      names it and a content hash would make the manifest wrong on every
//      build
//   2. every specifier the app's import map answers stays external, so the
//      page holds one Svelte runtime, one library and one SDK. Two runtimes
//      do not degrade, they crash with `effect_orphan` (earlier research
//      finding 25)
//   3. runes mode, because a component outside it imports
//      `svelte/internal/flags/legacy`, which the map does not answer
//   4. the styles travel inside that one module, because the manifest names
//      an entry and nothing else, so a second file has no way to reach the
//      document
//
// The fourth is what makes a module free to name any Tailwind utility
// (ADR-20260914-244 rules 10 to 13). The app's `@source` lines scan the app and the
// library's dist, and a module is absent from that build, so a utility only a
// module names is emitted by nobody. Tailwind runs here instead, over the
// module's own tree.
import { createHash } from 'node:crypto';

import type { Plugin, UserConfig } from 'vite';

import { modulePlugins } from './plugins.js';
import { ANY_MODULE, scopedToTheModule } from './scope.js';
import { SHARED_SPECIFIERS } from './specifiers.js';

// The addresses a module's bundle keeps bare. An application that writes the
// import map behind them needs the same list, so `./build` publishes it.
export { SHARED_SPECIFIERS } from './specifiers.js';
export { moduleDev, type ModuleDevOptions } from './dev.js';

/** Where Svelte's own code lives, in a checkout and under any package store. */
const SVELTE_PACKAGE = /[\\/]node_modules[\\/]svelte[\\/]/;

export interface ModuleBuildOptions {
  /** The module's entry file, relative to the project root. */
  entry: string;
  /**
   * The output file name, relative to `outDir`. It has to be the tail of the
   * manifest's `ui.entry`.
   */
  fileName?: string;
  /** Where the bundle is written, relative to the project root. */
  outDir?: string;
}

/**
 * Refuses a bundle the browser could not load, at the moment it is built
 * rather than the moment a person opens the page.
 *
 * Two things fail it. A module of Svelte's own in the output means the bundle
 * carries a second runtime. A bare import the map does not answer means the
 * browser stops on the module's first line.
 */
const refuseWhatTheMapCannotAnswer = (): Plugin => ({
  name: 'prusa-connect-ui:refuse-what-the-map-cannot-answer',
  generateBundle: {
    order: 'post',
    handler: (_options, bundle) => {
      for (const output of Object.values(bundle)) {
        if (output.type !== 'chunk') continue;

        const carried = output.moduleIds.filter((id) =>
          SVELTE_PACKAGE.test(id),
        );
        if (carried.length > 0) {
          throw new Error(
            `${output.fileName} carries Svelte's own code, so the page would hold two runtimes: ${carried.join(', ')}`,
          );
        }

        const unanswered = output.imports.filter(
          (specifier) =>
            !specifier.startsWith('.') &&
            !SHARED_SPECIFIERS.includes(specifier),
        );
        if (unanswered.length > 0) {
          throw new Error(
            `${output.fileName} imports what the app's import map does not answer: ${unanswered.join(', ')}`,
          );
        }
      }
    },
  },
});

/**
 * Puts the module's compiled CSS inside the entry chunk, as a style element
 * the bundle adds to the document when it is evaluated.
 *
 * `cssCodeSplit: false` makes Vite write the CSS as an asset beside the entry,
 * and nothing fetches it: the manifest names one file (ADR-20260914-244 rule 13). So
 * the asset leaves the bundle and its bytes travel in the one file the host
 * serves.
 *
 * The element carries a digest of those bytes as its id, and the injection
 * stops when an element with that id already stands. One bundle reached at two
 * URLs then styles the page once, and two modules get two elements.
 *
 * Both halves of the ordering are needed. `enforce: 'post'` puts this after
 * Vite's own css-post plugin, which is what creates the asset, and
 * `order: 'post'` puts this after that plugin's own `generateBundle`. A plain
 * user plugin runs before both and sees no asset at all.
 */
/**
 * The statements that put a bundle's styles into the page once.
 *
 * The host serves a bundle under /api/v1/modules/<namespace>/ui/, so the
 * bundle reads its namespace from its own address and narrows its styles to
 * its own markup and stops at another module's inside it (ADR-20261029-zzg).
 * Served from anywhere else, such as in a test, it keeps the scope of any
 * module. The element's id names the namespace, so two modules that ship one
 * stylesheet get one element each.
 */
const injectStyles = (id: string, css: string): string => `\
if (typeof document !== 'undefined') {
  const own = /\\/modules\\/([^/]+)\\/ui\\//.exec(import.meta.url)?.[1];
  const namespace = own === undefined ? undefined : decodeURIComponent(own);
  const id = namespace === undefined ? ${JSON.stringify(id)} : ${JSON.stringify(`${id}-`)} + namespace;
  if (!document.getElementById(id)) {
    const style = document.createElement('style');
    style.id = id;
    const css = ${JSON.stringify(css)};
    if (namespace === undefined) {
      style.textContent = css;
    } else {
      const self = '[data-module=' + JSON.stringify(namespace) + ']';
      style.textContent = css.replace(
        ${JSON.stringify(`@scope ${ANY_MODULE}{`)},
        '@scope (' + self + ') to ([data-module]:not(' + self + ')){',
      );
    }
    document.head.append(style);
  }
}
`;

const carryTheStylesInsideTheEntry = (): Plugin => ({
  name: 'prusa-connect-ui:carry-the-styles-inside-the-entry',
  enforce: 'post',
  generateBundle: {
    order: 'post',
    handler: (_options, bundle) => {
      let css = '';

      for (const [name, output] of Object.entries(bundle)) {
        if (output.type !== 'asset' || !name.endsWith('.css')) continue;

        css +=
          typeof output.source === 'string'
            ? output.source
            : Buffer.from(output.source).toString('utf8');
        Reflect.deleteProperty(bundle, name);
      }

      // Vite appends this marker to a CSS asset so that its file name's hash
      // moves when the CSS does. The asset is gone, so the marker is bytes
      // every module would carry for nothing.
      css = css.replace(/\/\*\$vite\$:\d+\*\//g, '');

      if (css.trim() === '') return;

      css = scopedToTheModule(css);

      const entry = Object.values(bundle).find(
        (output) => output.type === 'chunk' && output.isEntry,
      );
      if (entry?.type !== 'chunk') return;

      const id = `prusa-connect-ui-${createHash('sha256').update(css).digest('hex').slice(0, 16)}`;

      // A bundle is also evaluated under Node, by a test or by a tool reading
      // what it exports, and there is no document there. Statements ahead of
      // the chunk's `import` declarations are legal in an ES module: the
      // imports are hoisted and run first, and this runs after them.
      entry.code = injectStyles(id, css) + entry.code;
    },
  },
});

/**
 * The whole Vite config for a module's bundle. A module's `vite.config.ts` is
 * one line over it:
 *
 * ```ts
 * import { moduleBuild } from '@prusa/connect-ui/build';
 * import { defineConfig } from 'vite';
 *
 * export default defineConfig(moduleBuild({ entry: 'src/index.ts' }));
 * ```
 */
export const moduleBuild = ({
  entry,
  fileName = 'index.js',
  outDir = 'dist',
}: ModuleBuildOptions): UserConfig => ({
  plugins: [
    ...modulePlugins({ hmr: false }),
    refuseWhatTheMapCannotAnswer(),
    carryTheStylesInsideTheEntry(),
  ],
  build: {
    outDir,
    emptyOutDir: true,
    // The app serves the bundle to a browser that supports import maps, and
    // those browsers support everything Svelte 5 emits.
    target: 'esnext',
    cssCodeSplit: false,
    lib: {
      entry,
      formats: ['es'],
      fileName: () => fileName,
    },
    rolldownOptions: {
      external: [...SHARED_SPECIFIERS],
    },
  },
});
