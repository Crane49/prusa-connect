import { svelte } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'vite';

// One chunk per subpath of the `exports` map in package.json. A consumer that
// imports `./primitives` loads that chunk and none of the others.
const entries = {
  index: 'src/index.ts',
  domain: 'src/domain/index.ts',
  data: 'src/data/index.ts',
  i18n: 'src/i18n/index.ts',
  slots: 'src/slots/index.ts',
  primitives: 'src/primitives/index.ts',
  router: 'src/router/index.ts',
  shell: 'src/shell/index.ts',
  dashboard: 'src/shell/dashboard/index.ts',
  'setup-wizard': 'src/shell/setup-wizard/index.ts',
};

export default defineConfig({
  plugins: [tailwindcss(), svelte()],
  build: {
    // Beside the files scripts/emit-package.mjs writes, never over them: the
    // `svelte` condition points at dist/ and this build answers `import`, so
    // dist/index.js is the compiled source and dist/esm/index.js the chunk.
    outDir: 'dist/esm',
    lib: {
      entry: entries,
      formats: ['es'],
      fileName: (_format, name) => `${name}.js`,
    },
    rollupOptions: {
      // The consumer's Svelte runtime, Tailwind and SDK, never a copy of them.
      // Two runtimes on one page fail with `effect_orphan` (earlier research
      // finding 25). The runtime dependencies are external for the same
      // reason: bits-ui keeps a menu's open state in Svelte context, and two
      // copies of it on one page would not see each other.
      external: [
        'svelte',
        /^svelte\//,
        'tailwindcss',
        /^@prusa\/connect(\/|$)/,
        /^bits-ui(\/|$)/,
        /^svelte-sonner(\/|$)/,
        /^@lucide\/svelte(\/|$)/,
        // bits-ui imports this at the top of its entry for its date parts,
        // which nothing here renders, so it is a dependency that ships and
        // never runs.
        /^@internationalized\/date(\/|$)/,
      ],
    },
  },
});
