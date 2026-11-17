// Links inside a page open a screen, the way the navigation sweep proves the
// chrome's links do.
//
// The chrome sweep reads only the rail, the Settings rooms and the phone bar.
// A destination a person reaches from a row of a listing is invisible to it,
// and one such destination reached nothing for as long as the listing linked
// to it. So the links a page draws get a sweep of their own.
import type { Browser } from 'playwright';
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

it('a print named on the jobs page opens that print', async () => {
  const listing = await open(browser, url, '/jobs', { settle: 2000 });
  let href: string;
  try {
    expect(await listing.page.locator(NOT_FOUND).count()).toBe(0);
    href = await listing.page.evaluate(() => {
      const row = document.querySelector<HTMLAnchorElement>(
        'main a[href^="/jobs/"]',
      );

      return row?.getAttribute('href') ?? '';
    });
  } finally {
    await listing.close();
  }

  // A sweep that found no link would pass without opening anything.
  expect(href, 'the jobs page names no print').not.toBe('');

  const print = await open(browser, url, href, { settle: 2000 });
  try {
    expect(
      await print.page.locator(NOT_FOUND).count(),
      `${href} draws the not-found screen`,
    ).toBe(0);

    // The screen renders for a print the server does not hold too, and it
    // renders the word "No such print" there. A sweep that stopped at "a
    // screen drew" would pass against a link that reaches nothing.
    const body = (await print.page.locator('main').innerText()) || '';
    expect(body, `${href} says the server holds no such print`).not.toContain(
      'No such print',
    );
    expect(body, `${href} says the print is further back`).not.toContain(
      'This print is further back',
    );

    // The heading is the print's own name, which says the row was found and
    // not merely that a screen rendered.
    const heading = await print.page.locator('main h1').first().textContent();
    expect(heading?.trim()).not.toBe('');
  } finally {
    await print.close();
  }
});
