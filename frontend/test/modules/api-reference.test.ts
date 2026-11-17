// The API reference page with modules installed (ADR-20261027-zyf).
//
// The core serves the page itself, outside this app, so the page is read on
// the core's own address and not through the dev server. `examples/spools`
// ships an API document and `examples/nozzle-watch` ships none, so the
// switcher names the core and the spools module and nothing else.
import type { Browser, BrowserContext, Page } from 'playwright';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';

import { launch, VIEWPORT } from '../ux/measure.ts';

const SPOOLS = 'com.github.aux.acme.spools';
const NOZZLE = 'com.github.aux.acme.nozzle-watch';

const SWITCHER = 'nav[aria-label="API documents"]';
const CURRENT = `${SWITCHER} a[aria-current="page"]`;

let browser: Browser;
let context: BrowserContext;
let sheet: Page;

/** Every CSP refusal and page error the browser reports while the lane runs. */
const faults: string[] = [];

const core = (): string => {
  const url = process.env.PCL_CORE_URL ?? '';
  if (url === '') {
    throw new Error('PCL_CORE_URL names the core that serves the page');
  }

  return url.replace(/\/$/u, '');
};

/** The heading the renderer draws from the document's info.title. */
const heading = async (): Promise<string> => {
  const title = sheet.locator('.scalar-app h1').first();
  await title.waitFor({ state: 'visible', timeout: 60_000 });

  return (await title.innerText()).trim();
};

const path = (): string => {
  const url = new URL(sheet.url());

  return `${url.pathname}${url.search}`;
};

beforeAll(async () => {
  browser = await launch();
  context = await browser.newContext({ viewport: VIEWPORT });

  const password = process.env.PCL_DEMO_PASSWORD ?? '';
  if (password === '') {
    throw new Error('PCL_DEMO_PASSWORD carries the password the demo printed');
  }

  // The session cookie lands in the context, so the page is read signed in.
  const signIn = await context.request.post(`${core()}/api/v1/auth/login`, {
    data: { username: process.env.PCL_DEMO_USER ?? 'demo', password },
  });
  expect(signIn.ok()).toBe(true);

  sheet = await context.newPage();
  sheet.on('pageerror', (error) => faults.push(error.message));
  sheet.on('console', (message) => {
    if (message.type() === 'error') faults.push(message.text());
  });
});

afterAll(async () => {
  await context.close();
  await browser.close();
});

describe('the API reference page', () => {
  it('lists the core and each module that ships a document', async () => {
    await sheet.goto(`${core()}/docs`);

    const entries = await sheet.locator(`${SWITCHER} a`).allInnerTexts();
    expect(entries).toEqual(['Core API', 'Spools']);
    expect(await sheet.locator(CURRENT).innerText()).toBe('Core API');
    expect(
      await sheet.locator(`${SWITCHER} a[data-document="${NOZZLE}"]`).count(),
    ).toBe(0);
  });

  it('switches between the core and a module, and the history follows', async () => {
    await sheet.goto(`${core()}/docs`);
    const coreTitle = await heading();

    await sheet.locator(`${SWITCHER} a[data-document="${SPOOLS}"]`).click();
    await sheet.waitForURL(/module=/u);
    expect(path()).toBe(`/docs?module=${SPOOLS}`);
    expect(await sheet.locator(CURRENT).innerText()).toBe('Spools');
    expect(await heading()).toBe('spools');

    await sheet.goBack();
    expect(path()).toBe('/docs');
    expect(await heading()).toBe(coreTitle);

    await sheet.goForward();
    expect(path()).toBe(`/docs?module=${SPOOLS}`);
    expect(await heading()).toBe('spools');
  });

  it('opens a module selected from a deep link', async () => {
    await sheet.goto(`${core()}/docs?module=${SPOOLS}`);

    expect(await sheet.locator(CURRENT).innerText()).toBe('Spools');
    expect(await heading()).toBe('spools');
  });

  it('says in place that a module without a document has none', async () => {
    const answer = await sheet.goto(`${core()}/docs?module=${NOZZLE}`);

    expect(answer?.status()).toBe(404);
    expect(await sheet.getByRole('alert').innerText()).toContain(
      'This API document cannot be shown',
    );

    // The way back to the core's document is the switcher.
    await sheet.locator(`${SWITCHER} a[href="/docs"]`).click();
    await sheet.waitForURL(/\/docs$/u);
    expect(await heading()).not.toBe('');
  });

  it('keeps the switcher in view and on the page width', async () => {
    for (const width of [1280, 390]) {
      await sheet.setViewportSize({ width, height: 844 });
      await sheet.goto(`${core()}/docs?module=${SPOOLS}`);
      await heading();
      await sheet.mouse.wheel(0, 2000);

      const box = await sheet.locator(SWITCHER).boundingBox();
      expect(box?.y, `the switcher scrolled away at ${String(width)}`).toBe(0);
      expect(
        await sheet.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth,
        ),
        `the page scrolls sideways at ${String(width)}`,
      ).toBe(true);
    }
  });

  // The renderer asks api.scalar.com for a registry, and the policy refuses it
  // on purpose. Any other refusal is something this page added.
  it('trips the page policy on nothing but the refused registry', () => {
    const refusals = faults.filter((fault) =>
      /Content Security Policy/iu.test(fault),
    );

    expect(
      refusals.filter((fault) => !fault.includes('https://api.scalar.com/')),
    ).toEqual([]);
  });
});
