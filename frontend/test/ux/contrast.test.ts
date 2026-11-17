// Earlier research: a text colour is derived from the surface it sits on.
//
// Every visible text node on every route, in both themes, measured against the
// first opaque background above it, with every opacity on the way counted in.
// WCAG 2.1 1.4.3 asks 4.5:1, and 3:1 for text at 24 px or bold at 18.66 px.
//
// The component library's `src/styles/contrast.test.ts` pins the pairs the
// theme declares. This one measures what the browser actually painted, which
// is where a pair nobody declared shows up.
import type { Browser } from 'playwright';
import { afterAll, beforeAll, describe, expect, inject, it } from 'vitest';

import {
  contrastPairs,
  designSystemPairs,
  floorFor,
  isDark,
  launch,
  open,
} from './measure.ts';
import { ROUTES } from './routes.ts';

const url = inject('uxBaseUrl');

let browser: Browser;

beforeAll(async () => {
  browser = await launch();
});

afterAll(async () => {
  await browser.close();
});

for (const theme of ['light', 'dark'] as const) {
  describe(`every text pair in ${theme}`, () => {
    for (const route of ROUTES) {
      it(`${route.name} clears the line`, async () => {
        const sheet = await open(browser, url, route.path, {
          dark: theme === 'dark',
        });
        try {
          // The theme really is the one this run is about, so a dark sweep
          // that quietly measured the light page cannot pass.
          expect(await isDark(sheet.page)).toBe(theme === 'dark');

          const pairs = await contrastPairs(sheet.page);
          // A screen with no text at all would pass every assertion below
          // without measuring anything. The page draws a heading and a line
          // under it in one colour at one size, which the sweep counts as one
          // pair. Raise this number with the screens.
          expect(pairs.length).toBeGreaterThan(0);

          const exempt = await designSystemPairs(sheet.page);
          const under = pairs.filter(
            (pair) =>
              pair.ratio < floorFor(pair) &&
              !exempt.has(`${pair.color}|${pair.background}`),
          );
          const said = under
            .map((pair) =>
              [
                `${String(pair.ratio)}:1`,
                `at ${String(pair.size)}px`,
                `"${pair.sample}"`,
                `in ${pair.where}`,
              ].join(' '),
            )
            .join('\n');

          expect(under, said).toEqual([]);
        } finally {
          await sheet.close();
        }
      });
    }
  });
}
