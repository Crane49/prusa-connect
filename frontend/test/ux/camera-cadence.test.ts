// Earlier research: a camera frame is a feed with a cadence. The cadence is
// read from the strip of frames the fleet asks for, one request per frame, so
// the measure is the same one a person watching the screen makes.
import type { Browser } from 'playwright';
import { afterAll, beforeAll, expect, inject, it } from 'vitest';

import { launch, open } from './measure.ts';

const url = inject('uxBaseUrl');

/** How long the strip is watched for. Long enough to hold several frames. */
const WATCH_MS = 7000;

/** The slowest a feed may tick and still read as a feed rather than a picture. */
const SLOWEST_MS = 2500;

/** The fastest, so a broken tick that asks in a loop fails here. */
const FASTEST_MS = 200;

let browser: Browser;

beforeAll(async () => {
  browser = await launch();
});

afterAll(async () => {
  await browser.close();
});

// The frames are asked for by the Cameras view of the printers page, so the
// measure opens that view before it watches the strip.
it('every tile asks for a frame at a steady cadence', async () => {
  const sheet = await open(browser, url, '/printers');
  try {
    await sheet.page.getByRole('radio', { name: 'Cameras' }).click();

    const asked = new Map<string, number[]>();
    sheet.page.on('request', (request) => {
      const path = new URL(request.url()).pathname;
      const camera = /^\/api\/v1\/cameras\/([^/]+)\/snapshot$/.exec(path)?.[1];
      if (camera === undefined) return;
      asked.set(camera, [...(asked.get(camera) ?? []), Date.now()]);
    });

    await sheet.page.waitForTimeout(WATCH_MS);

    // Every printer in the fixture has a camera, so every card is on the
    // strip and none of them is stuck on its first frame.
    expect(asked.size).toBe(5);

    for (const [camera, moments] of asked) {
      expect(moments.length, camera).toBeGreaterThan(2);
      const gaps = moments
        .slice(1)
        .map((moment, index) => moment - (moments[index] ?? moment))
        .sort((one, two) => one - two);
      const median = gaps[Math.floor(gaps.length / 2)] ?? 0;

      expect(median, camera).toBeLessThanOrEqual(SLOWEST_MS);
      expect(median, camera).toBeGreaterThanOrEqual(FASTEST_MS);
    }
  } finally {
    await sheet.close();
  }
});
