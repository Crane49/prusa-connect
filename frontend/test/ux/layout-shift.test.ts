// Earlier research: nothing on a page moves when something loads or appears.
//
// Two measures. The first is cumulative layout shift from the first paint
// until the screen has settled, which is what a person sees while a screen
// fills. The second is the shift after a press, with the browser's 500 ms
// excuse for anything following a press removed, because a person sees that
// one too.
import type { Browser } from 'playwright';
import { afterAll, beforeAll, describe, expect, inject, it } from 'vitest';

import {
  cls,
  launch,
  moved,
  open,
  shiftAfter,
  type UxPage,
} from './measure.ts';
import { ROUTES } from './routes.ts';

const url = inject('uxBaseUrl');

/** What a press may move. Nothing, to the nearest hundredth. */
const PRESS_CEILING = 0.02;

let browser: Browser;

beforeAll(async () => {
  browser = await launch();
});

afterAll(async () => {
  await browser.close();
});

/** The fleet, with a press measured on it and the page closed after. */
const onTheFleet = async (
  press: (sheet: UxPage) => Promise<void>,
): Promise<{ shift: number; onThePage: string[]; sources: string }> => {
  const sheet = await open(browser, url, '/printers');
  try {
    const shift = await shiftAfter(sheet.page, async () => {
      await press(sheet);
    });
    // Each source says whether it moved on the page or on a surface over it.
    const sources = await moved(sheet.page);

    return {
      shift,
      onThePage: sources.filter((one) => one.startsWith('main ')),
      sources: sources.join(', '),
    };
  } finally {
    await sheet.close();
  }
};

describe('a screen that is filling moves nothing', () => {
  for (const route of ROUTES) {
    it(`${route.name} settles under ${String(route.ceiling)}`, async () => {
      const sheet = await open(browser, url, route.path);
      try {
        const seen = await cls(sheet.page);
        // The browser names the element behind each shift, so a failure says
        // what moved and from where rather than only that the page moved.
        expect(seen, (await moved(sheet.page)).join(', ')).toBeLessThan(
          route.ceiling,
        );
      } finally {
        await sheet.close();
      }
    });
  }
});

// Every press below reaches a control the fleet screen drew: the state
// filter, a card menu, the add-device dialog and the issues switch. Merge
// 41a1efb7 removed that screen, so the application draws none of them and
// there is nothing to press. The block is skipped rather than deleted, so
// the measure returns the day the screen does.
describe.skip('a press moves nothing but what it touches', () => {
  it('picking a state in the fleet filter leaves the grid where it is', async () => {
    const { shift, sources } = await onTheFleet(async (sheet) => {
      await sheet.page.getByRole('combobox', { name: /state/i }).click();
      await sheet.page.waitForTimeout(300);
      await sheet.page.getByRole('option').nth(1).click();
    });

    expect(shift, sources).toBeLessThan(PRESS_CEILING);
  });

  it('opening a card menu leaves the card where it is', async () => {
    const { shift, sources } = await onTheFleet(async (sheet) => {
      await sheet.page
        .getByRole('button', { name: /^More for/ })
        .first()
        .click();
    });

    expect(shift, sources).toBeLessThan(PRESS_CEILING);
  });

  it('opening the add-device dialog leaves the fleet where it is', async () => {
    const { onThePage, sources } = await onTheFleet(async (sheet) => {
      await sheet.page.getByRole('button', { name: /add/i }).first().click();
    });

    // The fleet under the dialog is what this press must not move. What the
    // dialog does to itself while it fills is the dialog's own measure, and
    // it belongs to whoever owns that screen.
    expect(onThePage, sources).toEqual([]);
  });

  it('switching the issues filter leaves the footer where it is', async () => {
    const { shift, sources } = await onTheFleet(async (sheet) => {
      await sheet.page.getByRole('switch').first().click();
    });

    expect(shift, sources).toBeLessThan(PRESS_CEILING);
  });
});
