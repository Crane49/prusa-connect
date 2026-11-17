// Ten tabs of one server in one browser, over plain HTTP on an address that
// is not a secure context, which is how a server on a LAN is reached.
//
// A browser opens at most six connections to one host over HTTP/1.1. Each tab
// used to read an event stream of its own, and a stream holds its connection
// while the tab is open. From the sixth tab on, no request of any tab got a
// connection, and the whole application stopped. The tabs now read one stream
// (ADR-20261023-a52). This lane proves it against a core that is really running:
//
//   - every tab loads its page
//   - one tab, and one only, holds a request to /api/v1/events
//   - a change made through the API reaches every tab as a live event
//   - when the tab holding the stream closes, another tab opens it, and the
//     next change reaches every tab that is still open
//
// The host name is mapped to 127.0.0.1 inside Chromium, so the page is not a
// secure context the way http://127.0.0.1 is. A browser offers some APIs
// only in a secure context, and a page that used one would pass on loopback
// and fail on a LAN.
import type { Browser, BrowserContext, Page, Request } from 'playwright';
import { chromium } from 'playwright';
import { afterAll, beforeAll, expect, inject, it } from 'vitest';

import { LAN_HOST } from './server.ts';

const TABS = 10;

const VIEWPORT = { width: 1280, height: 900 };

const need = (name: string): string => {
  const value = process.env[name];
  if (value === undefined || value === '') {
    throw new Error(`${name} is not set: run \`make journey-check\``);
  }

  return value;
};

const account = {
  user: need('PCL_DEMO_USER'),
  password: need('PCL_DEMO_PASSWORD'),
};

let browser: Browser;
let context: BrowserContext;
let base: string;

/** The requests to the event stream each tab has open now. */
const streams = new Map<Page, Set<Request>>();

const isStream = (request: Request): boolean =>
  new URL(request.url()).pathname === '/api/v1/events';

const watch = (page: Page): void => {
  const open = new Set<Request>();
  streams.set(page, open);
  page.on('request', (request) => {
    if (isStream(request)) open.add(request);
  });
  page.on('requestfinished', (request) => open.delete(request));
  page.on('requestfailed', (request) => open.delete(request));
  page.on('close', () => streams.delete(page));
};

/** The tabs that hold a stream open now. */
const reading = (): Page[] =>
  [...streams].filter(([, open]) => open.size > 0).map(([page]) => page);

const openStreams = (): number =>
  [...streams.values()].reduce((sum, open) => sum + open.size, 0);

let step = 0;
const say = (what: string): void => {
  step += 1;
  process.stdout.write(`    ${String(step).padStart(2)}. ${what}\n`);
};

beforeAll(async () => {
  base = inject('journeyBaseUrl').replace('127.0.0.1', LAN_HOST);
  browser = await chromium.launch({
    args: [`--host-resolver-rules=MAP ${LAN_HOST} 127.0.0.1`],
  });
  context = await browser.newContext({ viewport: VIEWPORT });
}, 180_000);

afterAll(async () => {
  await browser.close();
});

/** Asks the core over the session the browser holds, as the page does. */
const askTheCore = async <T>(
  page: Page,
  method: string,
  path: string,
  body?: unknown,
): Promise<T> => {
  const text = await page.evaluate(
    async ([how, where, sent]) => {
      const answer = await fetch(where, {
        method: how,
        headers: {
          accept: 'application/json',
          'content-type': 'application/json',
        },
        ...(sent === undefined ? {} : { body: sent }),
      });
      if (!answer.ok) {
        throw new Error(
          `${how} ${where} answered ${String(answer.status)}: ${await answer.text()}`,
        );
      }

      return await answer.text();
    },
    [method, path, body === undefined ? undefined : JSON.stringify(body)] as [
      string,
      string,
      string | undefined,
    ],
  );

  return JSON.parse(text) as T;
};

interface Fleet {
  readonly items: readonly { readonly id: string }[];
}

interface Dashboard {
  readonly id: number;
  readonly revision: number;
}

/** A dashboard with one printer's card and a note, so each tab reads both kinds of event. */
const draft = (device: string | undefined, note: string) => ({
  name: 'Many tabs',
  cards: [
    {
      id: 'n1',
      type: 'core/note',
      title: 'Note',
      config: { text: note },
      x: 0,
      y: 0,
      w: 4,
      h: 2,
    },
    ...(device === undefined
      ? []
      : [
          {
            id: 'p1',
            type: 'core/printer',
            title: '',
            config: { device },
            x: 4,
            y: 0,
            w: 4,
            h: 4,
          },
        ]),
  ],
});

it(`serves ${String(TABS)} tabs of one server over plain HTTP and hands the stream over`, async () => {
  // ── Sign in, in a tab of its own ───────────────────────────────────
  // That tab is closed and not navigated on. A stream it opened just before
  // it navigated would never be reported as ended or failed, and the count
  // below would hold it for ever.
  const signIn = await context.newPage();
  await signIn.goto(`${base}/`, { waitUntil: 'domcontentloaded' });
  expect(await signIn.evaluate(() => window.isSecureContext)).toBe(false);
  await signIn.getByRole('textbox', { name: 'Username' }).fill(account.user);
  await signIn
    .getByRole('textbox', { name: 'Password' })
    .fill(account.password);
  await signIn.getByRole('button', { name: 'Sign in' }).click();
  await signIn.waitForSelector('main', { timeout: 60_000 });
  say(
    `the demo account signs in on ${LAN_HOST}, which is not a secure context`,
  );

  // ── A dashboard every tab opens ────────────────────────────────────
  const fleet = await askTheCore<Fleet>(signIn, 'GET', '/api/v1/devices');
  const device = fleet.items[0]?.id;
  let dashboard = await askTheCore<Dashboard>(
    signIn,
    'POST',
    '/api/v1/dashboards',
    draft(device, 'first note'),
  );
  const address = `${base}/dashboards/${String(dashboard.id)}`;
  await signIn.close();
  say('a dashboard with a note and a printer card is made');

  // ── Ten tabs ───────────────────────────────────────────────────────
  const first = await context.newPage();
  watch(first);
  await first.goto(address);
  await first.getByText('first note').waitFor({ timeout: 60_000 });
  const others = await Promise.all(
    Array.from({ length: TABS - 1 }, async () => {
      const page = await context.newPage();
      watch(page);
      await page.goto(address, { waitUntil: 'domcontentloaded' });

      return page;
    }),
  );
  const tabs = [first, ...others];
  for (const page of tabs) {
    await page.getByText('first note').waitFor({ timeout: 60_000 });
  }
  say(`${String(TABS)} tabs load the dashboard`);

  await expect.poll(openStreams, { timeout: 30_000 }).toBe(1);
  // Long enough for a tab that would open a second stream to have done so.
  await new Promise((resolve) => setTimeout(resolve, 2_000));
  expect(openStreams()).toBe(1);
  const leader = reading()[0];
  expect(leader).toBeDefined();
  say('one tab holds the event stream, and the others hold none');

  // ── A live event reaches every tab ─────────────────────────────────
  dashboard = await askTheCore<Dashboard>(
    first,
    'PUT',
    `/api/v1/dashboards/${String(dashboard.id)}`,
    { ...draft(device, 'second note'), revision: dashboard.revision },
  );
  for (const page of tabs) {
    await page.getByText('second note').waitFor({ timeout: 30_000 });
  }
  say('a change of the dashboard reaches every tab');

  // ── The tab that reads closes, and another takes over ──────────────
  await leader?.close();
  const rest = tabs.filter((page) => page !== leader);
  await expect.poll(openStreams, { timeout: 30_000 }).toBe(1);
  const next = reading()[0];
  expect(next).toBeDefined();
  expect(next).not.toBe(leader);
  say('the tab holding the stream closes, and another tab opens it');

  await askTheCore<Dashboard>(
    rest[0] ?? first,
    'PUT',
    `/api/v1/dashboards/${String(dashboard.id)}`,
    { ...draft(device, 'third note'), revision: dashboard.revision },
  );
  for (const page of rest) {
    await page.getByText('third note').waitFor({ timeout: 30_000 });
  }
  say('the next change reaches every tab still open');

  await askTheCore<undefined>(
    rest[0] ?? first,
    'DELETE',
    `/api/v1/dashboards/${String(dashboard.id)}`,
  ).catch(() => undefined);
}, 300_000);
