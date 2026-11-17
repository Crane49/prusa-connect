// The application on a Vite dev server, in front of a core that is really
// running.
//
// The measure lane beside this one answers every request from a file. This one
// answers none of them: `scripts/check-journey.sh` starts `cmd/demo` with a
// simulated printer and a storage on disk, and every request the page makes is
// forwarded to it. So the sign-in cookie is set on the origin the page is on,
// and the request the browser makes is the request it makes with no proxy in
// front of it.
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import { createServer, type ViteDevServer } from 'vite';

import baseConfig from '../../vite.config.ts';

const here = dirname(fileURLToPath(import.meta.url));
const appRoot = join(here, '..', '..');

export interface JourneyServer {
  readonly url: string;
  close(): Promise<void>;
}

/**
 * A name Chromium maps to 127.0.0.1 in the lane that opens many tabs. A page
 * on it is not a secure context, as a server on a LAN address over plain HTTP
 * is not, while a page on 127.0.0.1 is one.
 */
export const LAN_HOST = 'pcl-lan.test';

/** The address of the core this lane drives. */
export const coreUrl = (): string => {
  const url = process.env.PCL_CORE_URL;
  if (url === undefined || url === '') {
    throw new Error('PCL_CORE_URL names no core: run `make journey-check`');
  }

  return url.replace(/\/$/, '');
};

/** The application on a port of the operating system's choosing. */
export const startJourneyServer = async (): Promise<JourneyServer> => {
  const core = coreUrl();

  const server: ViteDevServer = await createServer({
    ...baseConfig,
    configFile: false,
    root: appRoot,
    logLevel: 'error',
    server: {
      ...baseConfig.server,
      proxy: { '/api': { target: core, changeOrigin: false } },
      host: '127.0.0.1',
      port: 0,
      // Vite refuses a Host header it does not know.
      allowedHosts: [LAN_HOST],
    },
  });

  await server.listen();
  const url = server.resolvedUrls?.local[0];
  if (url === undefined) throw new Error('the dev server named no address');

  return { url: url.replace(/\/$/, ''), close: () => server.close() };
};
