import { expect, test } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import SettingsUpdatesScreen from './settings-updates-screen.svelte';
import { maintenanceSource, preparedUpgrade } from './story-source';

const TITLE = 'Restore these PostgreSQL databases yourself';

test('names each database a rollback left to the operator', async () => {
  await render(SettingsUpdatesScreen, {
    props: {
      source: maintenanceSource({
        upgrade: preparedUpgrade({
          state: 'rolled-back',
          external_restore: ['prusa_connect', 'farm_module'],
        }),
        history: [
          preparedUpgrade({
            id: 'online-0',
            state: 'rolled-back',
            external_restore: ['archive_db'],
          }),
        ],
      }),
    },
  });

  const titles = page.getByText(TITLE);
  await expect.element(titles.first()).toBeVisible();
  expect(titles.all()).toHaveLength(2);
  await expect.element(page.getByText('prusa_connect')).toBeVisible();
  await expect.element(page.getByText('farm_module')).toBeVisible();
  await expect.element(page.getByText('archive_db')).toBeVisible();
});

test('shows no restore warning when the server restored everything', async () => {
  await render(SettingsUpdatesScreen, {
    props: {
      source: maintenanceSource({
        upgrade: preparedUpgrade({ state: 'rolled-back' }),
        history: [preparedUpgrade({ id: 'online-0', state: 'rolled-back' })],
      }),
    },
  });

  await expect.element(page.getByText('rolled-back').first()).toBeVisible();
  expect(page.getByText(TITLE).query()).toBeNull();
});
