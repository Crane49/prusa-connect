// The Health tab: a printer away from the server reads as a normal
// condition, named and linked, and an optional instance that is not serving
// explains a count that reads short while the server is ready.
import { expect, test } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import SettingsServerScreen from './settings-server-screen.svelte';
import type { HealthSource } from './types';

const READY = {
  app: 'served',
  blocked: null,
  declared: 2,
  instances: [
    {
      resource: 'connector:xbuddy',
      state: 'running',
      required: true,
      serving: true,
    },
    {
      resource: 'module:fleet-stats',
      state: 'stopped',
      required: false,
      serving: false,
    },
  ],
  ready: true,
  serving: 1,
};

const sourceOf = (over: Partial<HealthSource> = {}): HealthSource => ({
  readiness: READY,
  connectors: [
    {
      connector: 'xbuddy',
      counts: { connected: 2, disconnected: 1 },
      degraded: [
        {
          id: '42d0d75d-ccb7-4e08-a001-f9e990838395',
          name: 'Workshop MK4',
          attempts: 0,
          last_seen: '2026-10-27T08:30:00.000Z',
          state: 'disconnected',
        },
      ],
      degraded_truncated: 0,
      status: 'degraded',
      total: 3,
    },
  ],
  readinessError: undefined,
  connectorsError: undefined,
  loaded: true,
  ...over,
});

test('names a printer that is away and links to its page', async () => {
  await render(SettingsServerScreen, { props: { source: sourceOf() } });

  await expect
    .element(page.getByRole('link', { name: 'Workshop MK4' }))
    .toHaveAttribute('href', '/printers/42d0d75d-ccb7-4e08-a001-f9e990838395');
  await expect.element(page.getByText(/^Last seen/)).toBeVisible();
  await expect.element(page.getByText('Some printers away')).toBeVisible();
  await expect
    .element(page.getByText(/Nothing on the server needs a restart/))
    .toBeVisible();
});

test('says nothing about printers away when every printer is connected', async () => {
  await render(SettingsServerScreen, {
    props: {
      source: sourceOf({
        connectors: [
          {
            connector: 'xbuddy',
            counts: { connected: 2 },
            degraded: [],
            degraded_truncated: 0,
            status: 'pass',
            total: 2,
          },
        ],
      }),
    },
  });

  await expect.element(page.getByText('All connected')).toBeVisible();
  await expect
    .element(page.getByText(/Nothing on the server needs a restart/))
    .not.toBeInTheDocument();
});

test('says all printers are away when none is connected', async () => {
  await render(SettingsServerScreen, {
    props: {
      source: sourceOf({
        connectors: [
          {
            connector: 'xbuddy',
            counts: { disconnected: 1 },
            degraded: [],
            degraded_truncated: 0,
            status: 'fail',
            total: 1,
          },
        ],
      }),
    },
  });

  await expect.element(page.getByText('All printers away')).toBeVisible();
});

test('lists the optional instances that are not serving', async () => {
  await render(SettingsServerScreen, { props: { source: sourceOf() } });

  await expect.element(page.getByText('module:fleet-stats')).toBeVisible();
  await expect
    .element(page.getByText('connector:xbuddy'))
    .not.toBeInTheDocument();
});
