import {
  createServer as createHttpServer,
  type IncomingMessage,
  type Server,
} from 'node:http';
import type { AddressInfo } from 'node:net';
import type { Duplex } from 'node:stream';
import { fileURLToPath } from 'node:url';

import { createServer, normalizePath, type ViteDevServer } from 'vite';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';

import {
  entryOf,
  externalizeShared,
  manifestNameOf,
  moduleDev,
  ownPaths,
} from './dev';
import { modulePlugins } from './plugins';

const FIXTURE = fileURLToPath(new URL('./testdata/module/', import.meta.url));

// Vite serves a file outside the root at /@fs/ and then its absolute path with
// forward slashes. On Windows that path starts with the drive letter.
const FIXTURE_FS = `/@fs/${normalizePath(FIXTURE).replace(/^\//, '')}`;

describe('externalizeShared', () => {
  let server: ViteDevServer;
  const errors: string[] = [];

  beforeAll(async () => {
    server = await createServer({
      root: FIXTURE,
      configFile: false,
      customLogger: {
        info: () => undefined,
        warn: () => undefined,
        warnOnce: () => undefined,
        error: (message: string) => errors.push(message),
        clearScreen: () => undefined,
        hasErrorLogged: () => false,
        hasWarned: false,
      },
      // `hmr: false` here would make vite-plugin-svelte force off the
      // compiler's own `hmr` option too, so the second assertion below never
      // sees `$.hmr`. Leaving Vite's HMR machinery at its default, unstarted
      // in middlewareMode without a real HTTP server, keeps both apart.
      server: { middlewareMode: true },
      plugins: [externalizeShared(), ...modulePlugins({ hmr: true })],
    });
  });

  afterAll(() => server.close());

  // The browser resolves these through the host's import map, so the dev
  // server has to hand them over exactly as the author wrote them.
  it('leaves Svelte and the library bare in a component', async () => {
    const result = await server.transformRequest('/src/page.svelte');
    const code = result?.code ?? '';

    expect(code).toMatch(/from ["']svelte\/internal\/client["']/);
    expect(code).not.toContain('/@id/');
    expect(code).not.toContain('node_modules');
    expect(code).toContain('$.hmr');
  });

  it('leaves the library bare in the entry', async () => {
    const result = await server.transformRequest('/src/index.ts');
    const code = result?.code ?? '';

    expect(code).toMatch(/from ["']@prusa\/connect-ui\/slots["']/);
    expect(code).not.toContain('/@id/');
  });

  // Vite pre-transforms every import it sees, and a shared specifier has no
  // file behind it here, so each one would print as a load failure.
  it('prints nothing about the specifiers it leaves to the host', async () => {
    await server.transformRequest('/src/page.svelte');
    await server.environments.client.waitForRequestsIdle();

    expect(errors).toEqual([]);
  });
});

const item = (name: string, namespace: string) => ({
  name,
  namespace,
  kind: 'asset',
  ui: { entry_url: `/api/v1/modules/${namespace}/ui/ui/index.js` },
});

describe('entryOf', () => {
  const listing = {
    items: [
      { name: 'past', kind: 'history' },
      item('spools', 'com.github.aux.acme.spools'),
    ],
  };

  it('finds the one install of a name', () => {
    expect(entryOf(listing, 'spools')).toEqual({
      found: {
        namespace: 'com.github.aux.acme.spools',
        entryUrl: '/api/v1/modules/com.github.aux.acme.spools/ui/ui/index.js',
      },
    });
  });

  it('says so when nothing by that name draws a UI', () => {
    expect(entryOf(listing, 'shelf')).toEqual({ missing: true });
    expect(
      entryOf({ items: [{ name: 'spools', kind: 'init' }] }, 'spools'),
    ).toEqual({ missing: true });
  });

  it('refuses to pick between two namespaces, and a namespace picks', () => {
    const two = { items: [item('spools', 'a.one'), item('spools', 'b.two')] };

    expect(entryOf(two, 'spools')).toEqual({ ambiguous: ['a.one', 'b.two'] });
    expect(entryOf(two, 'spools', 'b.two')).toEqual({
      found: {
        namespace: 'b.two',
        entryUrl: '/api/v1/modules/b.two/ui/ui/index.js',
      },
    });
  });

  it('reads anything that is not a listing as empty', () => {
    expect(entryOf('nope', 'spools')).toEqual({ missing: true });
    expect(entryOf({ items: 'nope' }, 'spools')).toEqual({ missing: true });
  });
});

describe('manifestNameOf', () => {
  it('reads the top-level name', () => {
    expect(manifestNameOf('# a comment\nname: spools\nkind: asset\n')).toBe(
      'spools',
    );
    expect(manifestNameOf('name: "spools"\n')).toBe('spools');
  });

  it('ignores a nested name', () => {
    expect(manifestNameOf('ui:\n  name: other\n')).toBeUndefined();
  });
});

describe('ownPaths', () => {
  it('keeps a custom entry directory off the core, not only src/', () => {
    const pattern = new RegExp(ownPaths('lib/index.ts', 'lib/locales'));

    expect(pattern.test('/lib/index.ts')).toBe(false);
    expect(pattern.test('/src/index.ts')).toBe(true);
    expect(pattern.test('/api/v1/resources')).toBe(true);
  });
});

const NAMESPACE = 'com.github.aux.acme.spools';
const ENTRY = `/api/v1/modules/${NAMESPACE}/ui/ui/index.js`;
const LISTING = JSON.stringify({ items: [item('spools', NAMESPACE)] });

/**
 * A core that answers the listing at `status` (200 by default), and 418 for
 * anything it does not know. `setStatus` swaps the status a later listing
 * answers with, for a test that watches the module react to the core
 * signing in.
 */
const fakeCore = async (
  listing: string,
  status = 200,
): Promise<{
  url: string;
  close: () => void;
  seen: string[];
  setStatus: (next: number) => void;
}> => {
  const seen: string[] = [];
  let code = status;
  const server: Server = createHttpServer((request, response) => {
    seen.push(request.url ?? '');
    if (request.url === '/api/v1/resources') {
      response.writeHead(code, { 'content-type': 'application/json' });
      response.end(listing);
      return;
    }
    if (request.url === '/') {
      response.writeHead(200, { 'content-type': 'text/html' });
      response.end('<!doctype html><title>core</title>');
      return;
    }
    response.writeHead(418);
    response.end();
  });
  // A real core answers a websocket upgrade it does not serve with bytes of
  // its own, which is what corrupts a socket two servers both answer.
  server.on('upgrade', (request: IncomingMessage, socket: Duplex) => {
    seen.push(request.url ?? '');
    socket.end('HTTP/1.1 400 Bad Request\r\n\r\n');
  });
  await new Promise<void>((done) => server.listen(0, '127.0.0.1', done));
  const { port } = server.address() as AddressInfo;

  return {
    url: `http://127.0.0.1:${String(port)}`,
    close: () => server.close(),
    seen,
    setStatus: (next: number) => {
      code = next;
    },
  };
};

/** A dev server over the fixture, in front of `core`, keeping what it warns. */
const devServer = async (
  core: string,
  warnings: string[] = [],
  hmr = false,
): Promise<{ dev: ViteDevServer; base: string }> => {
  const dev = await createServer({
    ...moduleDev({ core, manifest: 'module.yaml' }),
    root: FIXTURE,
    configFile: false,
    customLogger: {
      info: () => undefined,
      warn: (message: string) => warnings.push(message),
      warnOnce: (message: string) => warnings.push(message),
      error: (message: string) => warnings.push(message),
      clearScreen: () => undefined,
      hasErrorLogged: () => false,
      hasWarned: false,
    },
    server: { host: '127.0.0.1', port: 0, ...(!hmr && { hmr: false }) },
  });
  await dev.listen();

  return { dev, base: (dev.resolvedUrls?.local[0] ?? '').replace(/\/$/, '') };
};

describe('moduleDev, with hot replacement on', () => {
  // Vite's HMR socket connects at `/`, which the proxy also answers. Two
  // servers writing to one upgraded socket corrupt its frames, and the Vite
  // client reloads the page on every lost connection.
  it('keeps its own socket away from the core', async () => {
    const core = await fakeCore(LISTING);
    const { dev, base } = await devServer(core.url, [], true);
    try {
      // The token is read the way the browser reads it, out of Vite's client.
      const client = await (await fetch(`${base}/@vite/client`)).text();
      const token = /const wsToken = "([^"]+)"/.exec(client)?.[1] ?? '';
      const socket = new WebSocket(
        `${base.replace(/^http/, 'ws')}/?token=${token}`,
        'vite-hmr',
      );
      const first = await new Promise<string>((settle, fail) => {
        socket.addEventListener('message', (event) => {
          settle(String(event.data));
        });
        socket.addEventListener('error', () => {
          fail(new Error('the HMR socket failed'));
        });
      });
      // The core's answer arrives after Vite's first frame, so the socket is
      // given time to be corrupted before it is read back.
      await new Promise((settle) => setTimeout(settle, 500));

      expect(JSON.parse(first)).toEqual({ type: 'connected' });
      expect(socket.readyState).toBe(WebSocket.OPEN);
      expect(core.seen.filter((url) => url.startsWith('/?token'))).toEqual([]);
      socket.close();
    } finally {
      await dev.close();
      core.close();
    }
  });
});

describe('moduleDev', () => {
  let core: Awaited<ReturnType<typeof fakeCore>>;
  let dev: ViteDevServer;
  let base = '';

  beforeAll(async () => {
    core = await fakeCore(LISTING);
    ({ dev, base } = await devServer(core.url));
  });

  afterAll(async () => {
    await dev.close();
    core.close();
  });

  it('passes the listing through byte for byte', async () => {
    const body = await (await fetch(`${base}/api/v1/resources`)).text();
    expect(body).toBe(LISTING);
  });

  it('answers the installed entry from source, uncached', async () => {
    await (await fetch(`${base}/api/v1/resources`)).text();
    const response = await fetch(`${base}${ENTRY}`, { redirect: 'manual' });

    expect(response.status).toBe(302);
    expect(response.headers.get('location')).toBe('/src/index.ts');
    expect(response.headers.get('cache-control')).toBe('no-store');
    expect(core.seen).not.toContain(ENTRY);
  });

  it('answers the catalogue beside the entry from src/locales', async () => {
    await (await fetch(`${base}/api/v1/resources`)).text();
    const response = await fetch(
      `${base}/api/v1/modules/${NAMESPACE}/ui/ui/locales/en.json`,
    );

    expect(response.status).toBe(200);
    expect(await response.json()).toEqual({ title: 'Spools' });
  });

  it.each([
    '/@vite/client',
    `${FIXTURE_FS}src/index.ts`,
    '/src/index.ts',
    '/node_modules/.vite/deps/svelte.js',
    '/__vite_ping',
  ])('keeps %s away from the core', async (path) => {
    const response = await fetch(`${base}${path}`);

    expect(response.status).not.toBe(418);
    expect(core.seen).not.toContain(path);
  });

  // The bundle wraps its CSS in `@scope`; unscoped, a module's `.hidden`
  // would hide the host's own sidebar (ADR-20260914-244 rule 14).
  it('serves the module stylesheet scoped, as the bundle carries it', async () => {
    const served =
      await dev.environments.client.transformRequest('/src/styles.css');
    // Vite serves a stylesheet as a module holding the CSS as a JSON string.
    const css = JSON.parse(
      /const __vite__css = ("[^\n]*")/.exec(served?.code ?? '')?.[1] ?? '""',
    ) as string;

    expect(css).toMatch(/@scope \(\[data-module\]\)\{[^]*\.flex \{/);
    expect(css.startsWith('@layer properties;@property --tw-translate-x')).toBe(
      true,
    );
    expect(css.lastIndexOf('@property')).toBeLessThan(css.indexOf('@scope'));
  }, 60_000);

  it('sends everything else to the core', async () => {
    expect(await (await fetch(`${base}/`)).text()).toContain(
      '<title>core</title>',
    );
    expect((await fetch(`${base}/api/v1/devices`)).status).toBe(418);
  });
});

describe('moduleDev, with the module not installed', () => {
  it('says what to run, and leaves the page to the core', async () => {
    const core = await fakeCore(JSON.stringify({ items: [] }));
    const warnings: string[] = [];
    const { dev, base } = await devServer(core.url, warnings);
    try {
      await (await fetch(`${base}/api/v1/resources`)).text();

      expect(warnings.join('\n')).toMatch(
        /spools is not installed on .*: run `sh install.sh`/,
      );
      expect(await (await fetch(`${base}/`)).text()).toContain(
        '<title>core</title>',
      );
    } finally {
      await dev.close();
      core.close();
    }
  });
});

describe('moduleDev, with the core answering 401 before sign-in', () => {
  it('does not warn "not installed", and learns once the core signs in', async () => {
    const core = await fakeCore(LISTING, 401);
    const warnings: string[] = [];
    const { dev, base } = await devServer(core.url, warnings);
    try {
      await (await fetch(`${base}/api/v1/resources`)).text();
      expect(warnings.join('\n')).not.toContain('not installed');

      core.setStatus(200);
      await (await fetch(`${base}/api/v1/resources`)).text();
      const response = await fetch(`${base}${ENTRY}`, { redirect: 'manual' });
      expect(response.status).toBe(302);
      expect(response.headers.get('location')).toBe('/src/index.ts');
    } finally {
      await dev.close();
      core.close();
    }
  });
});
