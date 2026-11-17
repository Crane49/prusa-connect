// A file on its way to a printer, seen by everyone who may read that printer,
// against a core that is really running.
//
// Three people sign in, each in a browser of their own:
//
//   - the sender drops a file on a printer page
//   - a colleague who may run every printer sees the file in the header on
//     their own page, says who started it and may stop it, and sees it go
//     once it has landed
//   - a person who may read no printer sees nothing, in the header or in the
//     API
//
// The sender is the demo account. The other two are made here through the
// API, with a name new on every run.
import { mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import type { Browser, BrowserContext, Page } from 'playwright';
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

const sender = {
  user: need('PCL_DEMO_USER'),
  password: need('PCL_DEMO_PASSWORD'),
};
const deviceId = need('PCL_JOURNEY_DEVICE');

const run = String(Date.now());
const colleague = { user: `colleague-${run}`, password: `pass-${run}-c` };
const outsider = { user: `outsider-${run}`, password: `pass-${run}-o` };

/** One row as `GET /api/v1/transfers` lists it. */
interface RunningTransfer {
  readonly transfer_id: string;
  readonly device_id: string;
  readonly source: string;
  readonly state: string;
  readonly mine: boolean;
  readonly can_stop: boolean;
}

// Long enough that the printer, which fetches 64 KiB a second in this lane,
// is still fetching it while the other two look.
const fileName = `shared-${run}.gcode`;
const lines = Array.from(
  { length: 60_000 },
  (_, at) => `G1 X${String(at % 250)} Y${String((at * 7) % 210)} E0.01`,
);
const filePath = join(mkdtempSync(join(tmpdir(), 'pcl-shared-')), fileName);
writeFileSync(filePath, `; ${fileName}\n${lines.join('\n')}\n`);

let browser: Browser;
const contexts: BrowserContext[] = [];
let base: string;

let step = 0;
const say = (what: string): void => {
  step += 1;
  process.stdout.write(`    ${String(step).padStart(2)}. ${what}\n`);
};

beforeAll(async () => {
  base = inject('journeyBaseUrl');
  browser = await chromium.launch();
}, 180_000);

afterAll(async () => {
  for (const context of contexts) await context.close();
  await browser.close();
});

/** A browser of its own, so its cookie is one person's. */
const browserFor = async (): Promise<Page> => {
  const context = await browser.newContext({ viewport: VIEWPORT });
  contexts.push(context);

  return context.newPage();
};

const signIn = async (
  page: Page,
  person: { user: string; password: string },
): Promise<void> => {
  await page.goto(`${base}/`, { waitUntil: 'domcontentloaded' });
  await page.getByRole('textbox', { name: 'Username' }).fill(person.user);
  await page.getByRole('textbox', { name: 'Password' }).fill(person.password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await page
    .getByRole('button', { name: 'Sign in' })
    .waitFor({ state: 'detached', timeout: 60_000 });
};

/** Asks the core over the session the page holds. */
const ask = async <T>(
  page: Page,
  path: string,
  method = 'GET',
  body?: unknown,
): Promise<T> => {
  const answer = await page.evaluate(
    async ({ where, how, sent }) => {
      const read = await fetch(where, {
        method: how,
        headers: {
          accept: 'application/json',
          ...(sent === undefined ? {} : { 'content-type': 'application/json' }),
        },
        ...(sent === undefined ? {} : { body: JSON.stringify(sent) }),
      });
      if (!read.ok) throw new Error(`${where} answered ${String(read.status)}`);

      return await read.text();
    },
    { where: path, how: method, sent: body },
  );

  return (answer === '' ? undefined : JSON.parse(answer)) as T;
};

/** The header's button for the files on their way, by how many it counts. */
const indicator = (page: Page, count: number) =>
  page.getByRole('button', {
    name:
      count === 1 ? '1 file on its way' : `${String(count)} files on their way`,
  });

const runningOfTheFile = async (
  page: Page,
): Promise<RunningTransfer | undefined> => {
  const listed = await ask<{ items?: RunningTransfer[] }>(
    page,
    '/api/v1/transfers',
  );

  return (listed.items ?? []).find((one) => one.source.endsWith(fileName));
};

it('shows a file on its way to everyone who may read the printer, and to nobody else', async () => {
  // ── Three people ────────────────────────────────────────────────
  const sending = await browserFor();
  await signIn(sending, sender);
  await ask(sending, '/api/v1/auth/accounts', 'POST', {
    username: colleague.user,
    password: colleague.password,
    role: 'operator',
  });
  await ask(sending, '/api/v1/auth/accounts', 'POST', {
    username: outsider.user,
    password: outsider.password,
  });
  say(
    `the demo account makes ${colleague.user} an operator and ${outsider.user} with no role`,
  );

  const watching = await browserFor();
  await signIn(watching, colleague);
  await watching.goto(`${base}/storage`, { waitUntil: 'domcontentloaded' });
  const shut = await browserFor();
  await signIn(shut, outsider);
  say('the colleague and the outsider sign in, each in a browser of their own');

  // ── The sender drops a file ─────────────────────────────────────
  await sending.goto(`${base}/printers/${deviceId}/files`, {
    waitUntil: 'domcontentloaded',
  });
  const input = sending.locator('input[type="file"]').first();
  await input.waitFor({ state: 'attached', timeout: 60_000 });
  await input.setInputFiles(filePath);
  // A drop asks whether to print the file or only copy it.
  await sending.getByRole('radio', { name: /Only copy to printer/u }).click();
  await sending.getByRole('button', { name: 'Confirm' }).click();

  await expect
    .poll(async () => (await runningOfTheFile(sending))?.state, {
      timeout: 60_000,
      interval: 250,
    })
    .toBe('sending');
  expect((await runningOfTheFile(sending))?.mine).toBe(true);
  say(
    `the sender drops ${fileName} on the printer page, and the printer fetches it`,
  );

  // ── The colleague sees it ───────────────────────────────────────
  await indicator(watching, 1).click({ timeout: 30_000 });
  await watching.getByText(fileName).first().waitFor({ timeout: 30_000 });
  await watching.getByText('Started by someone else').waitFor();
  await watching
    .getByRole('button', { name: new RegExp(`^Stop sending ${fileName}`) })
    .waitFor();
  await watching.keyboard.press('Escape');

  const seen = await runningOfTheFile(watching);
  expect(seen?.mine).toBe(false);
  expect(seen?.can_stop).toBe(true);
  say(
    'the colleague sees the file in the header, started by someone else, and may stop it',
  );

  // ── The outsider sees nothing ───────────────────────────────────
  const listed = await ask<{ items?: RunningTransfer[] }>(
    shut,
    '/api/v1/transfers',
  );
  expect(listed.items ?? []).toEqual([]);
  // Long enough for a started or progress frame to have reached the page.
  await shut.waitForTimeout(3_000);
  expect(await indicator(shut, 1).count()).toBe(0);
  say('the outsider sees no file on its way, in the header or in the API');

  // ── It lands, and goes for everyone ─────────────────────────────
  await expect
    .poll(async () => await runningOfTheFile(sending), {
      timeout: 120_000,
      interval: 500,
    })
    .toBeUndefined();
  await indicator(watching, 1).waitFor({ state: 'detached', timeout: 30_000 });
  await indicator(sending, 1).waitFor({ state: 'detached', timeout: 30_000 });
  say(
    'the file lands, and the header lets it go for the sender and the colleague',
  );
}, 240_000);
