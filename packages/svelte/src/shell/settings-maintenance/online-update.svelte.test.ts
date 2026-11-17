import { expect, test, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import OnlineUpdate from './online-update.svelte';
import { maintenanceSource } from './story-source';

const limited = {
  code: 'core.resource/UPGRADE_SERVER_LIMITED',
  message: 'the update server asks to wait 300 seconds',
  params: { retry_after_seconds: 300 },
  status: 429,
  traceId: undefined,
};

test('says how long to wait when the update server limits this server', async () => {
  const refreshReleases = vi.fn(() => Promise.resolve());
  await render(OnlineUpdate, {
    props: {
      source: maintenanceSource({
        releaseError: limited,
        releaseRetryAt: Date.now() + 300_000,
        refreshReleases,
      }),
    },
  });

  await expect
    .element(page.getByText('Too many release requests'))
    .toBeVisible();
  await expect.element(page.getByText(/try again in 5 min/)).toBeVisible();
  await expect
    .element(page.getByRole('button', { name: 'Retry releases' }))
    .toBeDisabled();
  expect(page.getByText('core.resource/UPGRADE_SERVER_LIMITED').query()).toBe(
    null,
  );
});

test('allows a retry once the wait is over', async () => {
  const refreshReleases = vi.fn(() => Promise.resolve());
  await render(OnlineUpdate, {
    props: {
      source: maintenanceSource({
        releaseError: limited,
        releaseRetryAt: Date.now() - 1000,
        refreshReleases,
      }),
    },
  });

  await expect
    .element(
      page.getByText('The wait is over. You can ask for the releases again.'),
    )
    .toBeVisible();
  await page.getByRole('button', { name: 'Retry releases' }).click();

  expect(refreshReleases).toHaveBeenCalledOnce();
});

test('points at the pairing instead of repeating the refusal', async () => {
  await render(OnlineUpdate, {
    props: {
      source: maintenanceSource({
        releaseError: {
          code: 'core.resource/BETA_PORTAL_PAIRING_NEEDED',
          message:
            'pair this server with the Beta Portal to read online updates',
          params: {},
          status: 409,
          traceId: undefined,
        },
      }),
    },
  });

  await expect
    .element(page.getByText(/Beta Portal above, and its releases show here/))
    .toBeVisible();
  expect(
    page.getByText('core.resource/BETA_PORTAL_PAIRING_NEEDED').query(),
  ).toBe(null);
  expect(page.getByRole('button', { name: 'Retry releases' }).query()).toBe(
    null,
  );
});
