import { expect, test } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { maintenanceSource } from './story-source';
import SettingsBackupsScreen from './settings-backups-screen.svelte';

const POSTGRES_ACK =
  'I confirm that the PostgreSQL databases outside this backup are backed up.';

test('asks for no PostgreSQL acknowledgement where the backup covers every database', async () => {
  await render(SettingsBackupsScreen, {
    props: { source: maintenanceSource() },
  });

  await expect
    .element(page.getByRole('button', { name: 'Create backup' }))
    .toBeEnabled();
  expect(page.getByRole('checkbox', { name: POSTGRES_ACK }).query()).toBeNull();
});

test('asks for the PostgreSQL acknowledgement where a database lies outside the backup', async () => {
  const base = maintenanceSource();
  await render(SettingsBackupsScreen, {
    props: {
      source: maintenanceSource({
        version: { ...base.version!, postgres_backup_ack_required: true },
      }),
    },
  });

  await expect
    .element(page.getByRole('checkbox', { name: POSTGRES_ACK }))
    .toBeVisible();
});
