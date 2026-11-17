// Earlier research: a click changes only what it touches. Rule 10: a closed
// menu costs nothing in the document.
//
// Every press below is watched by a MutationObserver on `body`. A node added
// or removed outside the screen region the pressed control belongs to is
// counted, and a floating surface is not: a menu, a listbox and a dialog
// belong at the end of `body` and nowhere else.
import type { Browser } from 'playwright';
import { afterAll, beforeAll, describe, expect, inject, it } from 'vitest';

import { bodyNodes, changesOutside, launch, open } from './measure.ts';

const url = inject('uxBaseUrl');

/** The whole fleet screen: its controls, its count line and its grid. */
const SCREEN = 'main section';

/** The first printer's card. */
const CARD = 'main [data-device]';

let browser: Browser;

beforeAll(async () => {
  browser = await launch();
});

afterAll(async () => {
  await browser.close();
});

// Skipped, whole. Every case here presses a control of the fleet screen and
// reads what the press changed elsewhere: the state filter, the sort, a
// card's menu, and the links between two screens. Merge 41a1efb7 removed
// those screens, and the page that is drawn now carries no control at all,
// so there is no press to measure. The file stays so the measure returns
// with the first screen that draws one.
describe.skip('a press edits its own region and nothing else', () => {
  it('picking a state in the filter edits the fleet screen alone', async () => {
    const sheet = await open(browser, url, '/printers');
    try {
      const scope = await changesOutside(sheet.page, SCREEN, async () => {
        await sheet.page.getByRole('combobox', { name: /state/i }).click();
        await sheet.page.waitForTimeout(300);
        await sheet.page.getByRole('option').nth(1).click();
      });

      expect(scope, scope.examples.join(', ')).toMatchObject({
        added: 0,
        removed: 0,
      });
    } finally {
      await sheet.close();
    }
  });

  it('picking a sort edits the fleet screen alone', async () => {
    const sheet = await open(browser, url, '/printers');
    try {
      const scope = await changesOutside(sheet.page, SCREEN, async () => {
        await sheet.page.getByRole('combobox', { name: /sort/i }).click();
        await sheet.page.waitForTimeout(300);
        await sheet.page.getByRole('option').nth(1).click();
      });

      expect(scope, scope.examples.join(', ')).toMatchObject({
        added: 0,
        removed: 0,
      });
    } finally {
      await sheet.close();
    }
  });

  it("opening a card's menu edits that card and its own portal alone", async () => {
    const sheet = await open(browser, url, '/printers');
    try {
      const scope = await changesOutside(sheet.page, CARD, async () => {
        await sheet.page
          .getByRole('button', { name: /^More for/ })
          .first()
          .click();
      });

      expect(scope, scope.examples.join(', ')).toMatchObject({
        added: 0,
        removed: 0,
      });
    } finally {
      await sheet.close();
    }
  });

  it('a closed menu leaves nothing of its own in the document', async () => {
    const sheet = await open(browser, url, '/printers');
    try {
      const rest = await bodyNodes(sheet.page);

      await sheet.page
        .getByRole('button', { name: /^More for/ })
        .first()
        .click();
      await expect.poll(() => bodyNodes(sheet.page)).toBeGreaterThan(rest);

      await sheet.page.keyboard.press('Escape');

      // The menu leaves at the end of its exit animation, so this is polled
      // rather than read once.
      await expect.poll(() => bodyNodes(sheet.page)).toBe(rest);
    } finally {
      await sheet.close();
    }
  });

  it('a round trip through another screen leaves the document as it was', async () => {
    const sheet = await open(browser, url, '/printers');
    try {
      const before = await bodyNodes(sheet.page);

      await sheet.page.getByRole('link', { name: 'Tools' }).click();
      await sheet.page.waitForTimeout(1500);
      await sheet.page.getByRole('link', { name: 'Fleet' }).click();
      await sheet.page.waitForTimeout(2500);

      // A screen that is left takes its own portals with it, and a screen
      // that is opened twice costs what it cost the first time.
      expect(await bodyNodes(sheet.page)).toBe(before);
    } finally {
      await sheet.close();
    }
  });
});
