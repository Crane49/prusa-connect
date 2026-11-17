// The Vite dev server a module is developed behind (ADR-20260924-5nx).
import { readFileSync } from 'node:fs';
import type { IncomingMessage } from 'node:http';
import { resolve } from 'node:path';

import type {
  Connect,
  Plugin,
  ResolvedConfig,
  UserConfig,
  ViteDevServer,
} from 'vite';

import { modulePlugins } from './plugins.js';
import { scopedToTheModule } from './scope.js';
import { SHARED_SPECIFIERS } from './specifiers.js';

const isShared = (specifier: string): boolean =>
  SHARED_SPECIFIERS.includes(specifier);

/** `/@id/<specifier>`, quoted, as Vite writes an external it cannot serve. */
const PREFIXED = /(["'])\/@id\/([^"']+)\1/g;

/** The `Sec-WebSocket-Protocol` values Vite's own client connects with. */
const VITE_SOCKETS = ['vite-hmr', 'vite-ping'];

/**
 * Keeps every specifier the host's import map answers bare in what the dev
 * server sends, so the browser takes Svelte, the library and the SDK from the
 * host. Vite's client dev server has no externals (vitejs/vite#6582): an
 * external bare import comes out of `vite:import-analysis` as `/@id/svelte`.
 * The rewrite back has to run after that plugin, so `configResolved` moves it
 * to the end of the list. That is the placement
 * vite-plugin-externalize-dependencies uses.
 */
export const externalizeShared = (): Plugin => {
  const unprefix: Plugin = {
    name: 'prusa-connect-ui:unprefix-shared',
    transform: (code) =>
      code.includes('/@id/')
        ? code.replace(PREFIXED, (whole, quote: string, specifier: string) =>
            isShared(specifier) ? `${quote}${specifier}${quote}` : whole,
          )
        : null,
  };

  return {
    name: 'prusa-connect-ui:externalize-shared',
    enforce: 'pre',
    apply: 'serve',
    config: () => ({ optimizeDeps: { exclude: [...SHARED_SPECIFIERS] } }),
    configResolved: (config: ResolvedConfig) => {
      // `plugins` is read-only in the type and not at run time. This is the
      // one way to place a transform after Vite's own import analysis.
      (config.plugins as Plugin[]).push(unprefix);
    },
    resolveId: (specifier) =>
      isShared(specifier) ? { id: specifier, external: true } : null,
    // Vite pre-transforms every import a module names, external or not, and
    // a shared specifier has no file here, so each would be logged as a load
    // failure. The browser never asks for these ids: the import map answers
    // them from the host.
    load: (id) => (isShared(id) ? 'export {};' : null),
  };
};

/**
 * Scopes the module's stylesheets in dev as `moduleBuild()` does in the
 * bundle. Unscoped, the module's `.hidden` or `.flex-col` would reach the
 * host's own markup. It runs before Vite's CSS plugin and after Tailwind's,
 * which is why it has to follow `modulePlugins()` in the list: the CSS it sees
 * is already compiled.
 */
export const scopeStyles = (): Plugin => ({
  name: 'prusa-connect-ui:scope-styles',
  enforce: 'pre',
  apply: 'serve',
  transform: {
    filter: { id: /\.css(?:$|\?)/ },
    handler: (css) => scopedToTheModule(css),
  },
});

export interface InstalledModule {
  namespace: string;
  entryUrl: string;
}

export type EntryLookup =
  { found: InstalledModule } | { missing: true } | { ambiguous: string[] };

interface ListedInstall extends InstalledModule {
  name: string;
}

/** Every instance in a resources listing that draws a UI, narrowed from JSON. */
const installsOf = (listing: unknown): ListedInstall[] => {
  if (typeof listing !== 'object' || listing === null) return [];
  const { items } = listing as { items?: unknown };
  if (!Array.isArray(items)) return [];

  return items.flatMap((item: unknown) => {
    if (typeof item !== 'object' || item === null) return [];
    const { name, namespace, ui } = item as Record<string, unknown>;
    if (typeof name !== 'string' || typeof namespace !== 'string') return [];
    if (typeof ui !== 'object' || ui === null) return [];
    const { entry_url: entryUrl } = ui as Record<string, unknown>;
    if (typeof entryUrl !== 'string') return [];

    return [{ name, namespace, entryUrl }];
  });
};

/**
 * The install a dev server answers for. The core derives a namespace from the
 * module path at install, and the manifest carries none, so the core's
 * listing is the one place to read it.
 */
export const entryOf = (
  listing: unknown,
  name: string,
  namespace?: string,
): EntryLookup => {
  const named = installsOf(listing).filter(
    (install) =>
      install.name === name &&
      (namespace === undefined || install.namespace === namespace),
  );
  const [only] = named;

  if (only === undefined) return { missing: true };
  if (named.length > 1) {
    return { ambiguous: named.map((install) => install.namespace) };
  }

  return { found: { namespace: only.namespace, entryUrl: only.entryUrl } };
};

/**
 * The manifest's top-level `name`. One key is read, so a line is matched
 * rather than a YAML parser added to the library's dependencies.
 */
export const manifestNameOf = (yaml: string): string | undefined =>
  /^name:[ \t]*["']?([^"'\s#]+)/m.exec(yaml)?.[1];

export interface ModuleDevOptions {
  /** The module's entry, relative to the Vite root. */
  entry?: string;
  /** The catalogues the loader reads beside the entry. */
  locales?: string;
  /** The manifest, relative to the Vite root. */
  manifest?: string;
  /** Picks one install when a name is installed under two namespaces. */
  namespace?: string;
  /** Any origin serving the built app and its API, such as a core or `vite preview` of `frontend/`, not a Vite dev server, which claims the same paths as this one. */
  core?: string;
}

/** A string for literal use inside a `RegExp`'s source. */
const escapeForRegExp = (value: string): string =>
  value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

/** A path's first segment: the directory a module's own files live under. */
const firstSegment = (path: string): string => {
  const slash = path.indexOf('/');
  return slash === -1 ? path : path.slice(0, slash);
};

/**
 * Paths the dev server answers itself: Vite's own client and module graph,
 * `node_modules`, and the directories `entry` and `locales` live under.
 * Everything else is the core's. The directories are read from the options
 * rather than assumed to be `src/`, so a module that keeps its source
 * elsewhere still gets its own files from this server and not the core.
 */
export const ownPaths = (entry: string, locales: string): string => {
  const own = [...new Set([entry, locales].map(firstSegment))].map(
    (segment) => `${escapeForRegExp(segment)}/`,
  );

  return `^/(?!@vite/|@fs/|@id/|@react-refresh|node_modules/|__vite|${own.join('|')})`;
};

/**
 * Reads the listing as it passes to the browser, which is what tells this
 * server which namespace and entry the core gave the module. The response is
 * forwarded untouched, because the host reads the same bytes.
 */
const learnFrom = (
  response: IncomingMessage,
  learnt: (listing: unknown) => void,
): void => {
  // A 401 before sign-in is not a listing. Reading it as one would clear an
  // install already learnt and warn "not installed" while the core is only
  // waiting for credentials.
  if (response.statusCode !== 200) return;

  const chunks: Buffer[] = [];
  response.on('data', (chunk: Buffer) => chunks.push(chunk));
  response.on('end', () => {
    try {
      learnt(JSON.parse(Buffer.concat(chunks).toString('utf8')));
    } catch {
      // A listing that is not JSON is the host's to report, not this server's.
    }
  });
};

/** The dev server's own answers inside the installed entry's directory. */
const ownEntry = (
  options: Required<Omit<ModuleDevOptions, 'namespace' | 'core'>>,
  installed: () => InstalledModule | undefined,
): Plugin => ({
  name: 'prusa-connect-ui:own-entry',
  configureServer: (server: ViteDevServer) => {
    const locales = resolve(server.config.root, options.locales);

    // Added straight away, so it runs before Vite's proxy.
    server.middlewares.use(((request, response, next) => {
      const install = installed();
      const url = request.url?.split('?')[0] ?? '';
      if (install === undefined) {
        next();
        return;
      }

      const directory = install.entryUrl.slice(
        0,
        install.entryUrl.lastIndexOf('/') + 1,
      );
      if (url === install.entryUrl) {
        response.writeHead(302, {
          location: `/${options.entry}`,
          'cache-control': 'no-store',
        });
        response.end();
        return;
      }

      const catalogue = /^locales\/([\w-]+\.json)$/.exec(
        url.slice(directory.length),
      );
      if (url.startsWith(directory) && catalogue?.[1] !== undefined) {
        try {
          const body = readFileSync(resolve(locales, catalogue[1]));
          response.writeHead(200, {
            'content-type': 'application/json',
            'cache-control': 'no-store',
          });
          response.end(body);
        } catch {
          response.writeHead(404, { 'cache-control': 'no-store' });
          response.end();
        }
        return;
      }

      next();
    }) satisfies Connect.NextHandleFunction);
  },
});

/**
 * The whole Vite config for developing a module behind its own dev server
 * (ADR-20260924-5nx). A module's `vite.config.ts` picks it for `serve`:
 *
 * ```ts
 * export default ({ command }: { command: 'build' | 'serve' }) =>
 *   command === 'serve' ? moduleDev() : moduleBuild({ entry: 'src/index.ts' });
 * ```
 *
 * A plain function, not `defineConfig`: that helper is typed against the
 * module's own copy of Vite in its `node_modules`, a different type to the
 * compiler than the one this library's own Vite copy returns, so
 * `defineConfig` would reject `moduleDev()`'s result.
 */
export const moduleDev = ({
  entry = 'src/index.ts',
  locales = 'src/locales',
  manifest = '../module.yaml',
  namespace,
  core = process.env.PCL_CORE_URL ?? 'http://localhost:8080',
}: ModuleDevOptions = {}): UserConfig => {
  let installed: InstalledModule | undefined;
  let name: string | undefined;
  let told = '';
  let logger: ResolvedConfig['logger'] | undefined;

  const tell = (message: string): void => {
    if (message === told) return;
    told = message;
    logger?.warn(message);
  };

  const learnt = (listing: unknown): void => {
    if (name === undefined) return;
    const lookup = entryOf(listing, name, namespace);

    if ('found' in lookup) {
      installed = lookup.found;
      tell(`${name}: ${installed.entryUrl} is answered from ${entry}`);
    } else if ('missing' in lookup) {
      installed = undefined;
      tell(
        `${name} is not installed on ${core}: run \`sh install.sh\` once, then reload`,
      );
    } else {
      installed = undefined;
      tell(
        `${name} is installed under ${lookup.ambiguous.join(' and ')}: pick one with moduleDev({ namespace })`,
      );
    }
  };

  const manifestWatch: Plugin = {
    name: 'prusa-connect-ui:manifest',
    configResolved: (config) => {
      logger = config.logger;
      const path = resolve(config.root, manifest);
      name = manifestNameOf(readFileSync(path, 'utf8'));
      if (name === undefined) throw new Error(`${path} names no module`);
    },
    configureServer: (server) => {
      const path = resolve(server.config.root, manifest);
      // The manifest usually lives outside the Vite root (`../module.yaml`),
      // so it is not in the project's watched tree until added explicitly.
      // Vite's own watcher, unlike a bare `fs.watch`, survives an editor's
      // atomic save (write a temp file, rename over the original), and Vite
      // closes it itself, so this plugin does not track it to close later.
      server.watcher.add(path);
      server.watcher.on('change', (changed) => {
        if (changed !== path) return;
        // The core reads `contributes` when the module is installed, so a
        // manifest edit is invisible to it until the next install.
        tell(
          `${manifest} changed: run \`sh install.sh\` again for the core to read it`,
        );
      });
    },
  };

  // A `config` hook, not a literal `server.proxy` on the returned object: Vite
  // merges every plugin's `config()` result into the resolved config with
  // `mergeConfig`, which combines nested objects. A caller who spreads
  // `moduleDev()` and then writes its own `server` (as the dev server tests
  // do, to set `host`, `port` and `hmr`) replaces that key outright, and a
  // literal `server.proxy` here would be lost with it.
  const proxyToCore: Plugin = {
    name: 'prusa-connect-ui:proxy',
    config: () => ({
      server: {
        proxy: {
          [ownPaths(entry, locales)]: {
            target: core,
            changeOrigin: false,
            ws: true,
            // A LAN device answers on a certificate it signed itself, and
            // this is a dev server talking to a core its author named.
            secure: false,
            // Vite's own HMR socket upgrades at `/`, which this key matches
            // too. Both servers answering one socket corrupt its frames, so a
            // socket that names Vite's protocol is left to Vite: a string
            // result tells the proxy to step aside.
            bypass: (request) =>
              VITE_SOCKETS.includes(
                String(request.headers['sec-websocket-protocol']),
              )
                ? (request.url ?? '/')
                : undefined,
            configure: (proxy) => {
              proxy.on('proxyRes', (response, request) => {
                if (request.url?.split('?')[0] === '/api/v1/resources') {
                  learnFrom(response, learnt);
                }
              });
              proxy.on('error', () => {
                tell(`the core at ${core} does not answer: set PCL_CORE_URL`);
              });
            },
          },
        },
      },
    }),
  };

  return {
    plugins: [
      externalizeShared(),
      ...modulePlugins({ hmr: true }),
      scopeStyles(),
      manifestWatch,
      ownEntry({ entry, locales, manifest }, () => installed),
      proxyToCore,
    ],
  };
};
