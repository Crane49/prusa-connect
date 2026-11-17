// An app asks for a credential, a person approves it in the browser, and the
// token the app collects reaches only what the person approved (ADR-20261025-tpp),
// against a core that is really running.
//
// The app is this test, asking with no credential of its own. The person is
// the demo account, which administers the server, so a token that reached more
// than its scope would show it here.
import type { Browser, Page } from 'playwright';
import { chromium } from 'playwright';
import { afterAll, beforeAll, expect, inject, it } from 'vitest';

const VIEWPORT = { width: 1280, height: 900 };

const need = (name: string): string => {
  const value = process.env[name];
  if (value === undefined || value === '') {
    throw new Error(`${name} is not set: run \`make journey-check\``);
  }

  return value;
};

const person = {
  user: need('PCL_DEMO_USER'),
  password: need('PCL_DEMO_PASSWORD'),
};
const deviceId = need('PCL_JOURNEY_DEVICE');

/** The name the app gives itself, new on every run. */
const appName = `journey display ${String(Date.now())}`;

/** What the pairing request answers, as RFC 8628 section 3.2 names it. */
interface Issued {
  readonly device_code: string;
  readonly user_code: string;
  readonly verification_uri_complete: string;
  readonly interval: number;
}

/** What a poll answers once the person approved. */
interface Collected {
  readonly token: string;
  readonly scopes: readonly string[];
  readonly origin: string;
}

let browser: Browser;
let page: Page;
let base: string;

let step = 0;
const say = (what: string): void => {
  step += 1;
  process.stdout.write(`    ${String(step).padStart(2)}. ${what}\n`);
};

beforeAll(async () => {
  base = inject('journeyBaseUrl');
  browser = await chromium.launch();
  page = await (await browser.newContext({ viewport: VIEWPORT })).newPage();
}, 180_000);

afterAll(async () => {
  await browser.close();
});

/** One request as the app makes it: from outside the browser, with no cookie. */
const asTheApp = (
  method: string,
  path: string,
  body?: unknown,
  token?: string,
): Promise<Response> =>
  fetch(`${base}${path}`, {
    method,
    headers: {
      accept: 'application/json',
      ...(body === undefined ? {} : { 'content-type': 'application/json' }),
      ...(token === undefined ? {} : { authorization: `Bearer ${token}` }),
    },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });

it('approves an app in the browser and its token reaches only its scope', async () => {
  // ── The app asks ─────────────────────────────────────────────────
  const asked = await asTheApp('POST', '/api/v1/auth/app-pairings', {
    app_name: appName,
    scopes: ['read', 'operate'],
  });
  expect(asked.status).toBe(201);
  const issued = (await asked.json()) as Issued;
  say(`the app asks with no credential and shows ${issued.user_code}`);

  const waiting = await asTheApp('POST', '/api/v1/auth/app-pairings/token', {
    device_code: issued.device_code,
  });
  expect(waiting.status).toBe(400);
  expect(await waiting.text()).toContain('APP_PAIRING_PENDING');
  const polledAt = Date.now();
  say('its first poll is told nobody has decided yet');

  // ── The person opens the link the app shows ──────────────────────
  const link = new URL(issued.verification_uri_complete);
  await page.goto(`${base}${link.pathname}${link.search}`, {
    waitUntil: 'domcontentloaded',
  });
  await page.getByRole('textbox', { name: 'Username' }).fill(person.user);
  await page.getByRole('textbox', { name: 'Password' }).fill(person.password);
  await page.getByRole('button', { name: 'Sign in' }).click();

  await page
    .getByRole('heading', { name: `Request from ${appName}` })
    .waitFor({ timeout: 60_000 });
  say('the person signs in and reads which app asks');

  // The app asked to operate. The person gives it less.
  await page.getByRole('radio', { name: 'Read' }).check();
  await page.getByRole('button', { name: 'Approve' }).click();
  await page.getByText('Approved', { exact: true }).waitFor();
  say('the person narrows the scope to read and approves');

  // ── The app collects its token, keeping to the interval ──────────
  const wait = polledAt + issued.interval * 1000 + 200 - Date.now();
  if (wait > 0) await new Promise((done) => setTimeout(done, wait));

  const answer = await asTheApp('POST', '/api/v1/auth/app-pairings/token', {
    device_code: issued.device_code,
  });
  expect(answer.status).toBe(200);
  const collected = (await answer.json()) as Collected;
  expect(collected.scopes).toEqual(['read']);
  expect(collected.origin).toBe('app_pairing');
  say('the app collects a token limited to read');

  // ── The token reads and does nothing else ────────────────────────
  const read = await asTheApp(
    'GET',
    `/api/v1/devices/${deviceId}`,
    undefined,
    collected.token,
  );
  expect(read.status).toBe(200);
  say('the token reads the printer');

  const command = await asTheApp(
    'POST',
    `/api/v1/devices/${deviceId}/commands`,
    { verb: 'core.command.pause_print' },
    collected.token,
  );
  expect(command.status).toBe(403);

  const people = await asTheApp(
    'GET',
    '/api/v1/auth/accounts',
    undefined,
    collected.token,
  );
  expect(people.status).toBe(403);
  say('the token is refused a command and the list of people');

  // ── The person finds it among their credentials ──────────────────
  await page.getByRole('link', { name: 'See your credentials' }).click();
  await expect
    .poll(() => new URL(page.url()).pathname, { timeout: 30_000 })
    .toBe('/settings/tokens');
  await page.getByText(appName).first().waitFor({ timeout: 30_000 });
  say('the person finds the app among their credentials');
});
