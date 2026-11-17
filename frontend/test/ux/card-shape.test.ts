// Every card on the fleet is drawn to one shape, whatever its printer is
// doing. The owner's words: images no matter the printer status should be in
// the same position, so the layout is consistent.
//
// The measure is the row of readings, because it sits under the job line and
// the progress bar, which are the parts that change with the printer's state.
import type { Browser, Page } from 'playwright';
import { afterAll, beforeAll, describe, expect, inject, it } from 'vitest';

import { launch, open } from './measure.ts';

const url = inject('uxBaseUrl');

let browser: Browser;

beforeAll(async () => {
  browser = await launch();
});

afterAll(async () => {
  await browser.close();
});

/** Where each card's readings start, counted from that card's own top. */
const readingTops = (sheet: Page): Promise<number[]> =>
  sheet.evaluate(() =>
    [...document.querySelectorAll('[data-slot="printer-card"]')].map((card) => {
      const box = card.querySelector('dl');
      if (box === null) return -1;

      return Math.round(
        box.getBoundingClientRect().top - card.getBoundingClientRect().top,
      );
    }),
  );

describe('the fleet draws one card shape', () => {
  it('starts every row of readings at the same height in its card', async () => {
    const sheet = await open(browser, url, '/printers');
    try {
      const tops = await readingTops(sheet.page);

      expect(tops.length, 'the fleet drew no cards').toBeGreaterThan(1);
      expect(tops, 'a card drew no readings').not.toContain(-1);
      expect([...new Set(tops)], tops.join(', ')).toHaveLength(1);
    } finally {
      await sheet.close();
    }
  });
});
