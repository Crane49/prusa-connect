import { expect, test, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { pairingSource, phaseSource } from '../portal-pairing/story-source';
import SettingsMaintenanceScreen from './settings-maintenance-screen.svelte';
import type { MaintenanceSource } from './types';

const sourceOf = (
  over: Partial<MaintenanceSource> = {},
): MaintenanceSource => ({
  version: {
    build_profile: 'server',
    build_variant: 'full',
    capabilities: [],
    contract: '1.0.0',
    goarch: 'arm64',
    goos: 'linux',
    host_schema_compat: 10,
    host_schema_version: 12,
    managed_externally: false,
    version: '1.2.0',
  },
  upgrade: undefined,
  releases: [],
  releaseError: undefined,
  releaseRetryAt: undefined,
  releaseLoading: false,
  history: [],
  backups: [],
  backupError: undefined,
  backupLoading: false,
  deliveryAvailable: false,
  portal: undefined,
  offlineBundle: undefined,
  offlineUploadProgress: undefined,
  loaded: true,
  loading: false,
  busy: undefined,
  error: undefined,
  refreshReleases: vi.fn(() => Promise.resolve()),
  refreshBackups: vi.fn(() => Promise.resolve()),
  uploadOfflineBundle: vi.fn(() => Promise.resolve()),
  stage: vi.fn(() => Promise.resolve()),
  prepare: vi.fn(() => Promise.resolve()),
  apply: vi.fn(() => Promise.resolve()),
  cancel: vi.fn(() => Promise.resolve()),
  createBackup: vi.fn(() => Promise.resolve()),
  restoreBackup: vi.fn(() => Promise.resolve()),
  deleteBackup: vi.fn(() => Promise.resolve()),
  ...over,
});

test('shows the running build and empty maintenance collections', async () => {
  await render(SettingsMaintenanceScreen, { props: { source: sourceOf() } });

  await expect.element(page.getByText('1.2.0')).toBeInTheDocument();
  await expect
    .element(page.getByText('No newer release is available.'))
    .toBeInTheDocument();
  await expect
    .element(page.getByText('No backups have been created.'))
    .toBeInTheDocument();
});

test('keeps update methods and backups visible when the catalogue fails', async () => {
  await render(SettingsMaintenanceScreen, {
    props: {
      source: sourceOf({
        releaseError: {
          code: 'core.internal/UNEXPECTED',
          message: 'catalogue unavailable',
          params: {},
          status: 500,
          traceId: 'catalogue-trace',
        },
      }),
    },
  });

  await expect
    .element(page.getByRole('tab', { name: 'Offline' }))
    .toBeInTheDocument();
  await expect
    .element(page.getByText('No backups have been created.'))
    .toBeInTheDocument();
  await expect.element(page.getByText('catalogue-trace')).toBeInTheDocument();
  await expect
    .element(page.getByText('No newer release is available.'))
    .not.toBeInTheDocument();
});

test('starts portal pairing without asking the browser for a credential', async () => {
  const start = vi.fn(() => Promise.resolve());
  await render(SettingsMaintenanceScreen, {
    props: {
      source: sourceOf({
        deliveryAvailable: true,
        portal: pairingSource({ start }),
      }),
    },
  });

  await page.getByRole('button', { name: 'Start pairing' }).click();

  expect(start).toHaveBeenCalledOnce();
  expect(page.getByRole('textbox').query()).toBeNull();
});

test('shows only the safe fields for a pending portal pairing', async () => {
  await render(SettingsMaintenanceScreen, {
    props: {
      source: sourceOf({
        deliveryAvailable: true,
        portal: phaseSource('code'),
      }),
    },
  });

  await expect.element(page.getByText('WDJB-MJHT')).toBeInTheDocument();
  await expect
    .element(page.getByRole('link', { name: /Open the Beta Portal/ }))
    .toHaveAttribute(
      'href',
      'https://beta.prusa3d.com/device?user_code=WDJB-MJHT',
    );
  expect(page.getByRole('textbox').query()).toBeNull();
});

test('keeps releases visible when the backup listing fails', async () => {
  await render(SettingsMaintenanceScreen, {
    props: {
      source: sourceOf({
        backupError: {
          code: 'core.internal/UNEXPECTED',
          message: 'backups unavailable',
          params: {},
          status: 500,
          traceId: 'backup-trace',
        },
      }),
    },
  });

  await expect
    .element(page.getByText('No newer release is available.'))
    .toBeInTheDocument();
  await expect.element(page.getByText('backup-trace')).toBeInTheDocument();
  await expect
    .element(page.getByText('No backups have been created.'))
    .not.toBeInTheDocument();
});

test('shows offline artifacts and compatibility before prepare', async () => {
  const prepare = vi.fn(() => Promise.resolve());
  await render(SettingsMaintenanceScreen, {
    props: {
      source: sourceOf({
        deliveryAvailable: true,
        offlineUploadProgress: 100,
        offlineBundle: {
          name: 'Update 1.3.0',
          version: '1.3.0',
          artifacts: [
            {
              kind: 'core',
              name: 'prusa-connect',
              version: '1.3.0',
              size: 42_000_000,
            },
          ],
          impacts: [
            {
              extension: 'fleet-tools',
              outcome: 'disabled',
              reason: 'Requires host contract 2.',
            },
          ],
        },
        upgrade: {
          from_version: '1.2.0',
          id: 'offline-1',
          revision: 1,
          schema: 1,
          staged_binary: '/updates/candidate',
          state: 'staged',
          to_version: '1.3.0',
          updated_at: '2026-10-26T12:00:00Z',
        },
        prepare,
      }),
    },
  });

  await page.getByRole('tab', { name: 'Offline' }).click();
  await expect.element(page.getByText('prusa-connect')).toBeInTheDocument();
  await expect.element(page.getByText('Will be disabled')).toBeInTheDocument();
  await page
    .getByRole('button', { name: 'Run preflight and prepare' })
    .first()
    .click();

  expect(prepare).toHaveBeenCalledOnce();
});
