// The app on a Vite dev server whose `/api` calls go to a real core.
//
// The other two browser lanes answer the core from a file. This one cannot: it
// exists to prove that a module installed on a server is drawn by the app, and
// a fixture would prove that a fixture is drawn. `scripts/check-example-spools.sh`
// installs the two proof extensions and the refused fixture, starts the demo on
// them, and points this lane at it with PCL_CORE_URL.
//
// The dev server is what serves the import map, so a module's bundle finds one
// Svelte runtime and one component library at the addresses the map names.
import { existsSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import { createServer } from 'vite';

import baseConfig from '../../vite.config.ts';

const here = dirname(fileURLToPath(import.meta.url));
const appRoot = join(here, '..', '..');

/** Where the app reads the component library from. */
const LIBRARY = join(appRoot, '..', 'packages', 'svelte', 'dist', 'index.js');

export interface AppServer {
  readonly url: string;
  close(): Promise<void>;
}

/** The app on a port of the operating system's choosing, in front of one core. */
export const startAppServer = async (core: string): Promise<AppServer> => {
  if (!existsSync(LIBRARY)) {
    throw new Error(
      'the component library is not built: run `make client-ui-build`',
    );
  }

  const server = await createServer({
    ...baseConfig,
    configFile: false,
    root: appRoot,
    logLevel: 'error',
    server: {
      host: '127.0.0.1',
      port: 0,
      // Written out rather than taken from the app's own config, which reads
      // the address once when it is imported.
      proxy: { '/api': { target: core, changeOrigin: false } },
    },
  });

  await server.listen();
  const url = server.resolvedUrls?.local[0];
  if (url === undefined) throw new Error('the dev server named no address');

  return { url: url.replace(/\/$/u, ''), close: () => server.close() };
};
