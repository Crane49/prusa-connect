// The built app on a Vite preview server with a core that answers from a file.
//
// The app is built rather than served by the dev server: a fresh browser
// context asks a dev server for every module of the app one by one, up to a
// second and a half before a screen draws, and every measure opens one. The
// build itself takes a few seconds and is what a person gets.
import { existsSync, mkdtempSync, readFileSync, rmSync } from 'node:fs';
import type { IncomingMessage, ServerResponse } from 'node:http';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import { build, type Connect, preview } from 'vite';

import baseConfig from '../../vite.config.ts';

interface Answer {
  readonly status: number;
  readonly type: string;
  readonly body: string;
}

const here = dirname(fileURLToPath(import.meta.url));
const appRoot = join(here, '..', '..');

/** The answers the fake core gives, keyed by method and path. */
const FIXTURE = join(here, 'fixtures', 'api.json');

const problem = (status: number, code: string): Answer => ({
  status,
  type: 'application/problem+json',
  body: JSON.stringify({
    type: 'about:blank',
    status,
    code,
    title: 'refused',
    detail: `no fixture answers this`,
    params: {},
    retryable: false,
    trace_id: 'ux-trace',
  }),
});

// A frame the size the demo's camera serves.
const frame = (tick: number): string =>
  [
    '<svg xmlns="http://www.w3.org/2000/svg" width="640" height="480"',
    ' viewBox="0 0 640 480"><rect width="640" height="480" fill="hsl(',
    String((tick * 37) % 360),
    ' 60% 50%)"/></svg>',
  ].join('');

// Every answer waits this long.
const LATENCY_MS = 250;

const send = (response: ServerResponse, answer: Answer): void => {
  setTimeout(() => {
    response.statusCode = answer.status;
    response.setHeader('content-type', answer.type);
    response.setHeader('cache-control', 'no-store');
    response.end(answer.body);
  }, LATENCY_MS);
};

const api = (
  answers: Record<string, Answer>,
): ((server: { middlewares: Connect.Server }) => void) => {
  // How many frames each camera has served.
  const served = new Map<string, number>();

  return (server) => {
    // Added in the body of the hook.
    server.middlewares.use(
      (
        request: IncomingMessage,
        response: ServerResponse,
        next: () => void,
      ) => {
        const raw = request.url ?? '/';
        const path = raw.split('?')[0] ?? '/';
        if (!path.startsWith('/api/v1')) {
          next();
          return;
        }

        const method = request.method ?? 'GET';

        // The event stream is opened and never answered.
        if (path === '/api/v1/events') {
          response.statusCode = 200;
          response.setHeader('content-type', 'text/event-stream');
          response.setHeader('cache-control', 'no-store');
          response.write(': open\n\n');

          return;
        }

        const snapshot = /^\/api\/v1\/cameras\/([^/]+)\/snapshot$/.exec(path);
        if (snapshot) {
          const camera = decodeURIComponent(snapshot[1] ?? '');
          const tick = (served.get(camera) ?? 0) + 1;
          served.set(camera, tick);
          send(response, {
            status: 200,
            type: 'image/svg+xml',
            body: frame(tick),
          });

          return;
        }

        // A recorded key may carry the query.
        const asked = raw.slice(path.length);
        const query = asked === '' ? '' : decodeURIComponent(asked);
        send(
          response,
          answers[`${method} ${path}${query}`] ??
            answers[`${method} ${path}`] ??
            problem(404, 'core.api/NOT_FOUND'),
        );
      },
    );
  };
};

export interface UxServer {
  readonly url: string;
  close(): Promise<void>;
}

/** Where the app reads the component library from. */
const LIBRARY = join(appRoot, '..', 'packages', 'svelte', 'dist', 'index.js');

/** The app on a port of the operating system's choosing, with the fake core. */
export const startUxServer = async (): Promise<UxServer> => {
  // Said here rather than as a page that never draws.
  if (!existsSync(LIBRARY)) {
    throw new Error(
      'the component library is not built: run `make client-ui-build`',
    );
  }

  // Beside the checkout rather than in `dist/`, which is what the core embeds.
  const outDir = mkdtempSync(join(tmpdir(), 'pcl-ux-'));
  const config = { ...baseConfig, configFile: false as const, root: appRoot };

  await build({
    ...config,
    logLevel: 'error',
    build: { outDir, emptyOutDir: true },
  });

  // The fixture answers every request, so nothing is forwarded anywhere.
  const server = await preview({
    ...config,
    logLevel: 'error',
    plugins: [
      ...(baseConfig.plugins ?? []),
      {
        name: 'ux-core',
        configurePreviewServer: api(
          JSON.parse(readFileSync(FIXTURE, 'utf8')) as Record<string, Answer>,
        ),
      },
    ],
    build: { outDir },
    preview: { host: '127.0.0.1', port: 0, strictPort: false },
  });

  const url = server.resolvedUrls?.local[0];
  if (url === undefined) throw new Error('the preview server named no address');

  return {
    url: url.replace(/\/$/, ''),
    close: async () => {
      await server.close();
      rmSync(outDir, { recursive: true, force: true });
    },
  };
};
