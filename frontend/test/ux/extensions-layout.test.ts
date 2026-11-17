// The Extensions page with many modules installed, and an install question
// taller than the window.
//
// The fake core's answers are changed here to carry eleven installed modules,
// three of which another one requires, and an install plan that adds eight
// more. The measures are the page's own width, the table's box, and where the
// dialog's title and buttons are: the page never scrolls sideways, the table
// never scrolls inside its box, and the title and both buttons of the dialog
// stay inside the window while its body scrolls.
import type { Browser, Page } from 'playwright';
import { afterAll, beforeAll, describe, expect, inject, it } from 'vitest';

import { launch, open } from './measure.ts';

const url = inject('uxBaseUrl');

const SIZES = [
  { width: 1280, height: 860 },
  { width: 390, height: 844 },
] as const;

const PERMISSIONS = [
  'read_storage',
  'write_storage',
  'observe',
  'operate',
  'printers.read',
  'jobs.read',
];

const name = (index: number): string => `helper-${String(index)}`;

const offer = (index: number): Record<string, unknown> => ({
  module: `github.com/prusa3d/${name(index)}`,
  name: name(index),
  title: `Helper extension with a longer name ${String(index)}`,
  description:
    'Keeps a record of what each printer did and shows it on a page of its own.',
  author: 'Prusa Research',
  version: '1.4.0',
  source: `https://marketplace.example/${name(index)}-1.4.0.tgz`,
  namespace: `com.github.prusa3d.${name(index)}`,
  origin: 'marketplace',
  kind: 'driver',
  trust: 'official',
  installed: true,
  installed_version: '1.4.0',
  instance: `driver:${name(index)}`,
  requests_known: true,
  permissions: PERMISSIONS,
});

const instance = (index: number): Record<string, unknown> => ({
  config: {},
  enabled: true,
  generation: 1,
  kind: 'driver',
  name: name(index),
  namespace: `com.github.prusa3d.${name(index)}`,
  required: false,
  runtime: 'service',
  source: 'overlay',
  status: {
    attempt: 0,
    conditions: [],
    dropped: 0,
    observed_generation: 1,
    serving: true,
    state: 'ready',
  },
  type: 'module',
  uid: `ux-${String(index)}`,
  version: '1.4.0',
  module: {
    asked: {},
    title: `Helper extension with a longer name ${String(index)}`,
    version: '1.4.0',
    ...(index <= 3
      ? {
          required_by: [
            {
              instance: 'sink:planner',
              module: 'com.github.prusa3d.planner',
              version: '^1.0',
              enabled: true,
            },
          ],
        }
      : {}),
  },
});

/** The one module not installed yet, whose install adds eight more. */
const WANTED = 'github.com/prusa3d/project-board';

const wanted = {
  ...offer(0),
  module: WANTED,
  name: 'project-board',
  title: 'Project board',
  namespace: 'com.github.prusa3d.project-board',
  installed: false,
  installed_version: undefined,
  instance: undefined,
  requires: [{ module: 'com.github.prusa3d.helper-21', version: '^1' }],
};

const plan = {
  dependencies: Array.from({ length: 8 }, (_, index) => ({
    ...offer(index + 21),
    installed: false,
    installed_version: undefined,
    instance: undefined,
    events: { 'helper.ready': 'ready' },
    records_mib: 32,
    required_by: ['com.github.prusa3d.project-board ^1'],
  })),
  conflicts: [
    {
      module: 'com.github.prusa3d.project-board',
      with: 'com.github.prusa3d.helper-1',
      use: 'file_view',
      types: ['.3mf', '.stl', '.step'],
    },
  ],
};

const INSTALLED = Array.from({ length: 11 }, (_, index) => index + 1);

/** The core's answers with the modules in them. */
const manyModules = async (sheet: Page): Promise<void> => {
  await sheet.route(/\/api\/v1\/modules\/index(\?.*)?$/, (route) =>
    route.fulfill({
      json: {
        items: [wanted, ...INSTALLED.map(offer)],
        marketplace: {
          browse_url: 'https://marketplace.example',
          enabled: true,
          reachable: true,
          stale: false,
          url: 'https://marketplace.example/index.json',
          read_at: '2026-10-22T08:30:00Z',
        },
      },
    }),
  );
  await sheet.route(/\/api\/v1\/resources(\?.*)?$/, async (route) => {
    const response = await route.fetch();
    const body = (await response.json()) as { items: unknown[] };
    body.items = [...body.items, ...INSTALLED.map(instance)];
    await route.fulfill({ response, json: body });
  });
  await sheet.route(/\/api\/v1\/modules\/databases(\?.*)?$/, (route) =>
    route.fulfill({ json: { items: [] } }),
  );
  await sheet.route(/\/api\/v1\/modules\/plan$/, (route) =>
    route.fulfill({ json: plan }),
  );
};

/** How far the page is wider than the window, in pixels. */
const sideways = (sheet: Page): Promise<number> =>
  sheet.evaluate(
    () =>
      document.documentElement.scrollWidth -
      document.documentElement.clientWidth,
  );

/** How far a drawn table is wider than its box, in pixels. */
const tableSideways = (sheet: Page): Promise<number> =>
  sheet.evaluate(() =>
    Math.max(
      0,
      ...[...document.querySelectorAll('[data-slot="table-container"]')]
        .filter(
          (box) => box instanceof HTMLElement && box.offsetParent !== null,
        )
        .map((box) => box.scrollWidth - box.clientWidth),
    ),
  );

/** The parts of the dialog a person must reach, each inside the window or not. */
const dialogInView = (sheet: Page): Promise<Record<string, boolean>> =>
  sheet.evaluate(() => {
    const dialog = document.querySelector('[role="alertdialog"]');
    const inside = (element: Element | null): boolean => {
      if (element === null) return false;
      const box = element.getBoundingClientRect();

      return (
        box.top >= 0 &&
        box.left >= 0 &&
        box.bottom <= window.innerHeight &&
        box.right <= window.innerWidth
      );
    };
    const buttons = [
      ...(dialog?.querySelectorAll(
        '[data-slot="alert-dialog-footer"] button',
      ) ?? []),
    ];
    const body = dialog?.querySelector('[data-slot="dialog-body"]');

    return {
      title: inside(
        dialog?.querySelector('[data-slot="alert-dialog-title"]') ?? null,
      ),
      buttons: buttons.length === 2 && buttons.every(inside),
      bodyScrolls:
        body instanceof HTMLElement && body.scrollHeight > body.clientHeight,
    };
  });

let browser: Browser;

beforeAll(async () => {
  browser = await launch();
});

afterAll(async () => {
  await browser.close();
});

describe('the Extensions page with many modules', () => {
  for (const size of SIZES) {
    it(`keeps the listing inside the page at ${String(size.width)} px`, async () => {
      const sheet = await open(browser, url, '/settings/extensions', {
        ...size,
        prepare: manyModules,
      });
      try {
        await sheet.page
          .locator(`[data-extension="${WANTED}"]:visible`)
          .waitFor();

        expect(await sideways(sheet.page)).toBe(0);
        expect(await tableSideways(sheet.page)).toBe(0);
      } finally {
        await sheet.close();
      }
    });

    it(`keeps the install question's title and buttons in view at ${String(size.width)} px`, async () => {
      const sheet = await open(browser, url, '/settings/extensions', {
        ...size,
        prepare: manyModules,
      });
      try {
        const row = sheet.page.locator(`[data-extension="${WANTED}"]:visible`);
        await row.getByRole('button', { name: 'Install' }).click();
        await sheet.page
          .getByRole('alertdialog')
          .getByText('Helper extension with a longer name 28')
          .waitFor({ state: 'attached' });

        expect(await dialogInView(sheet.page)).toEqual({
          title: true,
          buttons: true,
          bodyScrolls: true,
        });
        expect(await sideways(sheet.page)).toBe(0);
      } finally {
        await sheet.close();
      }
    });
  }
});
