// The core an app story talks to: `fetch` answers from the UX fixture table,
// with the devices of the chosen fleet size and the story's own rows on top.
// A story installs it before the app's first request and restores the real
// `fetch` when it goes, since Storybook draws every story in one page.
import fixtures from '../ux/fixtures/api.json';
import {
  copiedFrom,
  devicesAnswer,
  type FleetSize,
} from '../../.storybook/fleet';
import { FLEET_STATS_FILES, historyAnswer } from './fleet-stats';
import recorded from './recorded.json';
import {
  type Answer,
  json,
  problem,
  stream,
  type Stream,
} from '../../src/test/stub';

export type { FleetSize };

/**
 * Rows a story puts over the table, keyed `"<METHOD> <path>"` as `api.json`
 * keys are, with the query when the row answers one query only. A function is
 * asked on every request, so `pending` in it keeps every request waiting.
 */
export type Overrides = Record<
  string,
  Answer | (() => Answer | Promise<Answer>)
>;

interface Row {
  readonly status: number;
  readonly type: string;
  readonly body: string;
}

// `recorded.json` holds what the demo core answers for the requests every
// screen makes that the UX table leaves out.
const table: Record<string, Row> = { ...recorded, ...fixtures };

const DEVICE_ID = /^\S+ \/api\/v1\/devices\/([^/?]+)/;

const UUID_HEAD =
  /([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-)[0-9a-f]{12}/g;

// A copied printer answers its jobs, files and history with the rows of the
// printer it was copied from, under its own id. Every other id in the rows
// takes the copy's tail as well: the jobs screen lists every printer's history
// in one keyed list, and two printers holding one job id would break it.
const copiedRow = (key: string): Row | undefined => {
  const id = DEVICE_ID.exec(key)?.[1] ?? '';
  const source = copiedFrom.get(id);
  if (source === undefined) return undefined;
  const row = table[key.replace(id, source)];

  const tail = id.slice(-12);

  return (
    row && {
      ...row,
      body: row.body
        .replaceAll(source, id)
        .replace(UUID_HEAD, (_, head: string) => `${head}${tail}`),
    }
  );
};

/** An answer that never comes: a loading state that stays loading. */
export const pending = (): Promise<never> =>
  new Promise<never>(() => undefined);

export const refused = (): Response => problem(403, 'forbidden');

export const failed = (): Response => problem(500, 'internal');

export const emptyList = (): Response =>
  json(200, { items: [], total: 0, limit: 50, offset: 0 });

const addressOf = (input: RequestInfo | URL): string => {
  if (typeof input === 'string') return input;
  return input instanceof URL ? input.href : input.url;
};

const methodOf = (input: RequestInfo | URL, init?: RequestInit): string =>
  init?.method ??
  (typeof input === 'object' && 'method' in input ? input.method : 'GET');

const fromRow = (row: Row): Response =>
  new Response(row.body, {
    status: row.status,
    headers: { 'content-type': row.type },
  });

// Read once, as this file loads, so a stub is never taken for the real one.
const original = globalThis.fetch;
const streams: Stream[] = [];

/** Puts the real `fetch` back and ends every event stream a stub opened. */
const restore = (): void => {
  globalThis.fetch = original;
  for (const events of streams.splice(0)) events.end();
};

/**
 * Answers the app from the table until the returned function is called. A
 * second install replaces the first stub, and the streams the first opened
 * stay open, so the app still mounted on them keeps its connection.
 */
export const installApi = (
  size: FleetSize,
  overrides: Overrides = {},
): (() => void) => {
  const answer = async (key: string): Promise<Response | undefined> => {
    const own = overrides[key];
    if (own !== undefined) {
      const given = typeof own === 'function' ? await own() : own;
      if (given instanceof Error) throw given;
      // A row may be asked for more than once and a body is read once.
      return given.clone();
    }
    if (key === 'GET /api/v1/devices') return devicesAnswer(size);
    if (key === 'GET /api/v1/history/facts') return historyAnswer(size);
    const row = table[key] ?? copiedRow(key);

    return row === undefined ? undefined : fromRow(row);
  };

  globalThis.fetch = async (input, init) => {
    // The extension's catalogues are files Storybook serves.
    if (addressOf(input).startsWith(FLEET_STATS_FILES)) {
      return original(input, init);
    }
    const url = new URL(addressOf(input), 'http://core.test');
    const method = methodOf(input, init);
    const path = `${method} ${url.pathname}`;

    // Held open, as the core holds it: a stream that ends or a 404 shows the
    // connection as lost on every screen.
    if (path === 'GET /api/v1/events' && overrides[path] === undefined) {
      const events = stream();
      streams.push(events);
      return events.response;
    }

    const query = decodeURIComponent(url.search);

    return (
      (query === '' ? undefined : await answer(`${path}${query}`)) ??
      (await answer(path)) ??
      problem(404, 'core.api/NOT_FOUND')
    );
  };

  return restore;
};
