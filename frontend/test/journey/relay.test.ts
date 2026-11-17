// A file sent to a printer from its page, and the page left while the file is
// on its way, against a core that is really running.
//
// The browser uploads the file into the server's storage, and the server then
// relays it to the printer. The relay used to run on the request of the page
// that asked for it, so a reload stopped it part way and the printer threw the
// file away. This lane proves the relay outlives the page:
//
//   - the page is left for another page, and the file is still drawn on its
//     way in the header
//   - the page is reloaded while the printer is fetching the file, and no
//     question stops the reload, because the browser holds none of the bytes
//   - after the reload the header draws the file again, until it lands
//   - the printer lists the file at its whole length
//   - the server deletes its own copy once the file has landed, because the
//     person did not ask to keep it
//
// A second file is sent with "Save to storage" on, into a folder picked in
// the page, and it stays in that folder after it lands.
//
// `scripts/check-journey.sh` starts the demo with `-transfer-rate`, so the
// simulated printer fetches the file slowly enough for the reload to land
// while the file is on its way.
import { mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

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

const account = {
  user: need('PCL_DEMO_USER'),
  password: need('PCL_DEMO_PASSWORD'),
};
const deviceId = need('PCL_JOURNEY_DEVICE');

/** One transfer as `GET /api/v1/devices/{id}/transfers` lists it. */
interface Transfer {
  readonly storage: string;
  readonly source: string;
  readonly path: string;
  readonly state: string;
  readonly bytes_sent: number;
  readonly bytes_total: number;
}

/** One entry as `GET /api/v1/devices/{id}/files` lists it. */
interface PrinterFile {
  readonly path: string;
  readonly size?: number;
}

// A file several blocks long, so the printer fetches it in several requests
// and the reload lands between two of them. Its name is new on every run, so
// the printer never holds a file of that name already.
const fileName = `relay-${String(Date.now())}.gcode`;
const lines = Array.from(
  { length: 120_000 },
  (_, at) => `G1 X${String(at % 250)} Y${String((at * 7) % 210)} E0.01`,
);
const body = `; ${fileName}\n${lines.join('\n')}\n`;
const fileBytes = Buffer.byteLength(body);
const filePath = join(mkdtempSync(join(tmpdir(), 'pcl-relay-')), fileName);
writeFileSync(filePath, body);

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
  const context = await browser.newContext({ viewport: VIEWPORT });
  page = await context.newPage();
}, 180_000);

afterAll(async () => {
  await browser.close();
});

/** Asks the core the same way the page does, over the session the browser holds. */
const askTheCore = async <T>(path: string): Promise<T> => {
  const answer = await page.evaluate(async (where: string) => {
    const read = await fetch(where, {
      headers: { accept: 'application/json' },
    });
    if (!read.ok) throw new Error(`${where} answered ${String(read.status)}`);

    return await read.text();
  }, path);

  return JSON.parse(answer) as T;
};

/** The newest transfer of this run's file, or undefined before it opens. */
const relayOfTheFile = async (): Promise<Transfer | undefined> => {
  const listed = await askTheCore<{ items?: Transfer[] }>(
    `/api/v1/devices/${deviceId}/transfers`,
  );

  return (listed.items ?? []).find((one) => sourceNames(one, fileName));
};

/**
 * Whether a transfer's source is a file of this name: a temporary copy the
 * server put in a folder of its own, or a file kept under its own name.
 */
const sourceNames = (transfer: Transfer, name: string): boolean =>
  transfer.source === name || transfer.source.endsWith(`/${name}`);

/** The status a read of one stored file answers, 200 or 404. */
const storedStatus = (storage: string, path: string): Promise<number> =>
  page.evaluate(
    async ({ at, where }: { at: string; where: string }) => {
      const read = await fetch(
        `/api/v1/storages/${encodeURIComponent(at)}/stat?path=${encodeURIComponent(where)}`,
        { headers: { accept: 'application/json' } },
      );

      return read.status;
    },
    { at: storage, where: path },
  );

/** The header's button for the files on their way, by how many it counts. */
const indicator = (count: number) =>
  page.getByRole('button', {
    name:
      count === 1 ? '1 file on its way' : `${String(count)} files on their way`,
  });

it('lands the whole file on the printer after the page that sent it is left and reloaded', async () => {
  // A question before a reload is a dialog. None may come: the relay needs
  // nothing from the page, and an upload still going up is not left here.
  const dialogs: string[] = [];
  page.on('dialog', (dialog) => {
    dialogs.push(dialog.type());
    void dialog.dismiss();
  });

  await page.goto(`${base}/`, { waitUntil: 'domcontentloaded' });
  await page.getByRole('textbox', { name: 'Username' }).fill(account.user);
  await page.getByRole('textbox', { name: 'Password' }).fill(account.password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await page.waitForSelector('main', { timeout: 60_000 });
  say('the demo account signs in');

  // ── The printer's Files tab ──────────────────────────────────────
  await page.goto(`${base}/printers/${deviceId}/files`, {
    waitUntil: 'domcontentloaded',
  });
  const input = page.locator('input[type="file"]').first();
  await input.waitFor({ state: 'attached', timeout: 60_000 });
  await input.setInputFiles(filePath);
  // A drop asks whether to print the file or only copy it.
  await page.getByRole('radio', { name: /Only copy to printer/u }).click();
  await page.getByRole('button', { name: 'Confirm' }).click();
  say(
    `${fileName} (${String(fileBytes)} bytes) is dropped on the printer page`,
  );

  // ── The relay starts ─────────────────────────────────────────────
  await expect
    .poll(async () => (await relayOfTheFile())?.state, {
      timeout: 60_000,
      interval: 250,
    })
    .toBe('sending');
  say('the file is in the server storage and on its way to the printer');

  // ── Another page ─────────────────────────────────────────────────
  await page.getByRole('link', { name: 'Storage' }).first().click();
  await expect.poll(() => new URL(page.url()).pathname).toBe('/storage');
  await indicator(1).waitFor({ timeout: 30_000 });
  say('another page still draws the file on its way in the header');

  // ── The reload ───────────────────────────────────────────────────
  const before = await relayOfTheFile();
  expect(before?.state).toBe('sending');
  expect(before?.bytes_sent ?? 0).toBeLessThan(fileBytes);

  await page.reload({ waitUntil: 'domcontentloaded' });
  await page.waitForSelector('main', { timeout: 60_000 });
  expect(dialogs).toEqual([]);
  say(
    `the page reloads at ${String(before?.bytes_sent ?? 0)} bytes, and nothing asks first`,
  );

  await indicator(1).click({ timeout: 30_000 });
  await page.getByText(fileName).first().waitFor({ timeout: 30_000 });
  await page.getByText('Sending to the printer').first().waitFor();
  await page.keyboard.press('Escape');
  say('after the reload the header draws the file on its way again');

  // ── The file lands whole ─────────────────────────────────────────
  await expect
    .poll(async () => (await relayOfTheFile())?.state, {
      timeout: 120_000,
      interval: 500,
    })
    .toBe('done');

  const landed = await relayOfTheFile();
  expect(landed?.bytes_total).toBe(fileBytes);
  // The count of bytes put on the wire, a range the printer asked for again
  // included, so it is at least the file and can be more.
  expect(landed?.bytes_sent).toBeGreaterThanOrEqual(fileBytes);

  const listing = await askTheCore<{ items?: PrinterFile[] }>(
    `/api/v1/devices/${deviceId}/files?path=usb&refresh=true`,
  );
  const onPrinter = (listing.items ?? []).find((one) =>
    one.path.endsWith(`/${fileName}`),
  );
  expect(onPrinter?.size).toBe(fileBytes);
  say(`the printer lists ${fileName} at its whole length`);

  await indicator(1).waitFor({ state: 'detached', timeout: 30_000 });
  say('the header lets the file go once it has landed');

  // ── The server's copy is gone ────────────────────────────────────
  expect(landed?.source).not.toBe(fileName);
  await expect
    .poll(() => storedStatus(landed?.storage ?? '', landed?.source ?? ''), {
      timeout: 30_000,
      interval: 250,
    })
    .toBe(404);
  const root = await askTheCore<{ items?: { name: string }[] }>(
    `/api/v1/storages/${encodeURIComponent(landed?.storage ?? '')}/files`,
  );
  expect((root.items ?? []).map((one) => one.name)).not.toContain(fileName);
  say('the server deleted its copy, and the Storage page never listed it');
}, 240_000);

it('keeps a file in the folder the person picked when Save to storage is on', async () => {
  const keptName = `kept-${String(Date.now())}.gcode`;
  const keptBody = `; ${keptName}\n${lines.slice(0, 2000).join('\n')}\n`;
  const keptPath = join(mkdtempSync(join(tmpdir(), 'pcl-kept-')), keptName);
  writeFileSync(keptPath, keptBody);
  const folder = `journey-${String(Date.now())}`;

  // The folder the file is kept in, made the way the Storage page makes one.
  const storages = await askTheCore<{
    items?: { name: string; write: boolean }[];
  }>('/api/v1/storages');
  const storage = (storages.items ?? []).find((one) => one.write)?.name ?? '';
  const made = await page.evaluate(
    async ({ at, path }: { at: string; path: string }) => {
      const answer = await fetch(
        `/api/v1/storages/${encodeURIComponent(at)}/folders`,
        {
          method: 'POST',
          headers: { 'content-type': 'application/json' },
          body: JSON.stringify({ path }),
        },
      );

      return answer.status;
    },
    { at: storage, path: folder },
  );
  expect(made).toBeLessThan(300);
  say(`the storage ${storage} holds a folder ${folder}`);

  await page.goto(`${base}/printers/${deviceId}/files`, {
    waitUntil: 'domcontentloaded',
  });
  await page
    .getByRole('switch', { name: 'Save to storage' })
    .click({ timeout: 60_000 });
  await page.getByRole('button', { name: 'Choose folder' }).click();
  const picker = page.getByRole('dialog', { name: 'Save the file in' });
  await picker.getByRole('button', { name: folder }).click();
  await picker.getByRole('button', { name: 'Save here' }).click();
  await page.getByText(folder).first().waitFor();
  say(`Save to storage is on, with ${folder} picked`);

  const input = page.locator('input[type="file"]').first();
  await input.setInputFiles(keptPath);
  // A drop asks whether to print the file or only copy it.
  await page.getByRole('radio', { name: /Only copy to printer/u }).click();
  await page.getByRole('button', { name: 'Confirm' }).click();
  say(`${keptName} is dropped on the printer page`);

  const keptRelay = async (): Promise<Transfer | undefined> => {
    const listed = await askTheCore<{ items?: Transfer[] }>(
      `/api/v1/devices/${deviceId}/transfers`,
    );

    return (listed.items ?? []).find((one) => sourceNames(one, keptName));
  };

  await expect
    .poll(async () => (await keptRelay())?.state, {
      timeout: 120_000,
      interval: 500,
    })
    .toBe('done');
  expect((await keptRelay())?.source).toBe(`${folder}/${keptName}`);
  say('the file landed on the printer from the folder it was saved in');

  // Longer than the deletion of a temporary copy takes, which the first
  // case waits for.
  await page.waitForTimeout(2_000);
  expect(await storedStatus(storage, `${folder}/${keptName}`)).toBe(200);
  say(`${keptName} is still in ${folder} after it landed`);
}, 240_000);
