// Every destination the application offers draws a screen.
//
// This is the measure that was missing. Nine rail entries reached the
// not-found screen at once, every unit test passed, and nothing said so: the
// route table and the screen table were each right about themselves. So the
// sweep reads the links out of the chrome the application drew, opens each
// one, and fails on the screen that says this build answers none.
import type { Browser, Page } from 'playwright';
import { afterAll, beforeAll, expect, inject, it } from 'vitest';

import { launch, open } from './measure.ts';

const url = inject('uxBaseUrl');

/** The screen that answers a path this build has no screen for. */
const NOT_FOUND = '[data-slot="not-found"]';

let browser: Browser;

beforeAll(async () => {
  browser = await launch();
});

afterAll(async () => {
  await browser.close();
});

/**
 * Every address the chrome offers: the rail, the Settings rooms folded under
 * it, and the phone bar. Read off the page rather than out of the route
 * table, because the route table is one of the two things this is checking.
 */
const offered = (sheet: Page): Promise<string[]> =>
  sheet.evaluate(() => {
    const page = document.querySelector('main');
    const links = [...document.querySelectorAll('a[href^="/"]')]
      .filter((link) => page?.contains(link) !== true)
      .map((link) => link.getAttribute('href') ?? '');

    return [...new Set(links)].filter((href) => href !== '');
  });

/**
 * The links the chrome draws, with every folded section opened first. The
 * Settings rooms sit behind a disclosure, and a room nobody unfolds is a room
 * this sweep would never open.
 */
const chromeAt = async (path: string): Promise<string[]> => {
  const sheet = await open(browser, url, path, { settle: 1500 });
  try {
    expect(
      await sheet.page.locator(NOT_FOUND).count(),
      `${path} draws the not-found screen`,
    ).toBe(0);

    // Only what a person can press: the phone bar draws its own copy of the
    // chrome, hidden at this width.
    const folded = sheet.page.locator('nav [aria-expanded="false"]:visible');
    for (let at = 0; at < (await folded.count()); at += 1) {
      await folded.nth(at).click({ force: true });
    }
    await sheet.page.waitForTimeout(400);

    return await offered(sheet.page);
  } finally {
    await sheet.close();
  }
};

it('every navigation entry opens a screen', async () => {
  const addresses = await chromeAt('/');

  // A sweep that found no link would pass without opening anything. The rail
  // draws Printers, Jobs, Storage, Tags and Settings, and the Settings entry
  // folds five rooms under it.
  expect(addresses.length).toBeGreaterThanOrEqual(9);

  // Every address at once, each in a context of its own.
  const drawn = await Promise.all(
    addresses.map(async (href) => {
      const sheet = await open(browser, url, href, { settle: 1500 });
      try {
        return {
          href,
          blank: (await sheet.page.locator(NOT_FOUND).count()) > 0,
        };
      } finally {
        await sheet.close();
      }
    }),
  );
  const blank = drawn.filter((one) => one.blank).map((one) => one.href);

  expect(blank, `these draw the not-found screen: ${blank.join(', ')}`).toEqual(
    [],
  );
});
