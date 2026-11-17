// The declaration files for dist/. `vite build` writes the JavaScript and
// nothing types a `.svelte` file, so this runs svelte2tsx's emitter over
// tsconfig.build.json and writes one `.d.ts` per source file into dist/,
// component props included.
import { createRequire } from 'node:module';
import { resolve } from 'node:path';

import { emitDts } from 'svelte2tsx';

const require = createRequire(import.meta.url);

await emitDts({
  svelteShimsPath: require.resolve('svelte2tsx/svelte-shims-v4.d.ts'),
  declarationDir: 'dist',
  libRoot: 'src',
  // svelte2tsx reads a relative path against libRoot, so this one is absolute.
  tsconfig: resolve('tsconfig.build.json'),
});
