// A long name stays inside the page at phone and tablet widths.
//
// The fixture's names are short, so the core's answers are changed here to
// carry a long dashboard name and a printer with a long file name on the bed.
// The measure is the page's own width and the boxes on the printer's page:
// the page never scrolls sideways, and the print's controls never draw over
// its name.
import type { Browser, Page } from 'playwright';
import { afterAll, beforeAll, describe, expect, inject, it } from 'vitest';

import { launch, open } from './measure.ts';

const url = inject('uxBaseUrl');

const DASHBOARD_NAME =
  'Second shift wall for the north hall, every printer and every camera';
const FILE_NAME =
  'Customer_order_4471_enclosure_panel_left_reinforced_v12_FINAL.bgcode';
const PRINTER = '34e039fc-13fd-4f4a-b16b-92392643f63a';

const WIDTHS = [390, 768] as const;

interface Named {
  name?: string;
  items?: { name: string }[];
}

interface Fleet {
  items: {
    id: string;
    state: Record<string, unknown>;
    job?: Record<string, unknown>;
  }[];
}

/** The core's answers with the long names in them. */
const longNames = async (sheet: Page): Promise<void> => {
  await sheet.route(/\/api\/v1\/dashboards(\/\d+)?(\?.*)?$/, async (route) => {
    const response = await route.fetch();
    const body = (await response.json()) as Named;
    if (body.items === undefined) body.name = DASHBOARD_NAME;
    else
      body.items = body.items.map((row) => ({ ...row, name: DASHBOARD_NAME }));
    await route.fulfill({ response, json: body });
  });

  await sheet.route(/\/api\/v1\/devices(\?.*)?$/, async (route) => {
    const response = await route.fetch();
    const body = (await response.json()) as Fleet;
    const printer = body.items.find((device) => device.id === PRINTER);
    if (printer !== undefined) {
      printer.state = {
        ...printer.state,
        '/state': 'PRINTING',
        '/job/state': 'PRINTING',
        '/job/progress': 42,
        '/job/path': `/usb/${FILE_NAME}`,
        '/job/time_remaining': 3600,
      };
      printer.job = {
        ...printer.job,
        path: `/usb/${FILE_NAME}`,
        display_name: FILE_NAME,
        state: 'PRINTING',
        progress: 42,
        running: true,
        time_remaining_ms: 3_600_000,
      };
    }
    await route.fulfill({ response, json: body });
  });
};

/** How far the page is wider than the window, in pixels. */
const sideways = (sheet: Page): Promise<number> =>
  sheet.evaluate(
    () =>
      document.documentElement.scrollWidth -
      document.documentElement.clientWidth,
  );

/**
 * How many of the print's controls cross the file name. The name is measured
 * by its parts, the clipped head and the tail, because the tail never shrinks
 * and is the part that runs out of a box too narrow for it.
 */
const overlap = (sheet: Page): Promise<number> =>
  sheet.evaluate((file) => {
    const name = document.querySelector(`[title="${file}"]`);
    const stop = [...document.querySelectorAll('button')].find((button) =>
      /stop/i.test(button.textContent),
    );
    const controls = stop?.parentElement;
    if (name === null || controls === null || controls === undefined) {
      return -1;
    }
    const crosses = (a: DOMRect, b: DOMRect): boolean =>
      a.left < b.right &&
      b.left < a.right &&
      a.top < b.bottom &&
      b.top < a.bottom;
    const parts = [...name.children].map((part) =>
      part.getBoundingClientRect(),
    );

    return [...controls.children]
      .map((control) => control.getBoundingClientRect())
      .filter((box) => parts.some((part) => crosses(part, box))).length;
  }, FILE_NAME);

let browser: Browser;

beforeAll(async () => {
  browser = await launch();
});

afterAll(async () => {
  await browser.close();
});

describe('a long name stays inside the page', () => {
  for (const width of WIDTHS) {
    it(`keeps the dashboard at ${String(width)} px`, async () => {
      const sheet = await open(browser, url, '/', {
        width,
        prepare: longNames,
      });
      try {
        const name = sheet.page.locator('[data-part="dashboard-switcher"]');
        await name.waitFor();

        expect(await name.getAttribute('title')).toBe(DASHBOARD_NAME);
        expect(await sideways(sheet.page)).toBe(0);
      } finally {
        await sheet.close();
      }
    });

    it(`keeps the printer's page at ${String(width)} px`, async () => {
      const sheet = await open(browser, url, `/printers/${PRINTER}`, {
        width,
        prepare: longNames,
      });
      try {
        const name = sheet.page.locator(`[title="${FILE_NAME}"]`).first();
        await name.waitFor();
        await sheet.page
          .getByRole('button', { name: /stop/i })
          .first()
          .waitFor();

        expect(await overlap(sheet.page), 'a control draws over the name').toBe(
          0,
        );
        expect(await sideways(sheet.page)).toBe(0);
      } finally {
        await sheet.close();
      }
    });
  }
});
