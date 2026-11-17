import { expect, test, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import {
  maintenanceSource,
  preparedUpgrade,
  reviewsNeedingApproval,
} from './story-source';
import type { UpgradeSchedule } from '../../data';
import UpdateTransaction from './update-transaction.svelte';

const APPROVE =
  'I approve what these extensions ask for and the versions they install.';

test('marks new requests and an older version in the bundled extensions', async () => {
  await render(UpdateTransaction, {
    props: {
      source: maintenanceSource({
        upgrade: preparedUpgrade({ extension_reviews: reviewsNeedingApproval }),
      }),
    },
  });

  await expect
    .element(page.getByText('Bundled extensions'))
    .toBeInTheDocument();
  await expect
    .element(page.getByText('Installed 1.5.2, bundle has 1.4.0'))
    .toBeInTheDocument();
  await expect
    .element(page.getByText('Older version', { exact: true }))
    .toBeInTheDocument();
  const asks = page.getByRole('list', {
    name: 'What github.com/example/fleet-tools asks for',
  });
  await expect
    .element(asks.getByText('New', { exact: true }).first())
    .toBeVisible();
  expect(asks.getByText('New', { exact: true }).all()).toHaveLength(2);
  await expect
    .element(page.getByText('Goes back from 1.5.2 to the older 1.4.0.'))
    .toBeInTheDocument();
  await expect
    .element(
      page.getByText('New requests: send commands to printers, read cameras'),
    )
    .toBeInTheDocument();
});

test('applies only after the operator approves the bundled extensions', async () => {
  const apply = vi.fn(() => Promise.resolve());
  await render(UpdateTransaction, {
    props: {
      source: maintenanceSource({
        apply,
        upgrade: preparedUpgrade({ extension_reviews: reviewsNeedingApproval }),
      }),
    },
  });

  const button = page.getByRole('button', { name: 'Apply update' });
  await expect.element(button).toBeDisabled();

  await page.getByRole('checkbox', { name: APPROVE }).click();
  await expect.element(button).toBeEnabled();
  await button.click();

  expect(apply).toHaveBeenCalledWith(
    expect.objectContaining({ approve_extensions: true }),
  );
});

test('sends no approval when no bundled extension needs one', async () => {
  const apply = vi.fn<(schedule: UpgradeSchedule) => Promise<void>>(() =>
    Promise.resolve(),
  );
  await render(UpdateTransaction, {
    props: {
      source: maintenanceSource({
        apply,
        upgrade: preparedUpgrade({
          extension_reviews: [
            {
              module: 'github.com/example/fleet-tools',
              version: '2.1.0',
              installed_version: '2.0.0',
              asks: ['read printers'],
            },
          ],
        }),
      }),
    },
  });

  await expect
    .element(page.getByText('github.com/example/fleet-tools'))
    .toBeInTheDocument();
  expect(page.getByRole('checkbox', { name: APPROVE }).query()).toBeNull();
  await page.getByRole('button', { name: 'Apply update' }).click();

  expect(apply).toHaveBeenCalledOnce();
  expect(apply.mock.calls[0]?.[0]).not.toHaveProperty('approve_extensions');
});

test('offers no apply on an update that already rolled back', async () => {
  await render(UpdateTransaction, {
    props: {
      source: maintenanceSource({
        upgrade: preparedUpgrade({ state: 'rolled-back' }),
      }),
    },
  });

  await expect
    .element(page.getByRole('button', { name: 'Dismiss' }))
    .toBeInTheDocument();
  expect(page.getByRole('button', { name: 'Apply update' }).all()).toHaveLength(
    0,
  );
  expect(
    page.getByRole('button', { name: 'Run preflight and prepare' }).all(),
  ).toHaveLength(0);
});

test('says why a container cannot apply and how to update it', async () => {
  const base = maintenanceSource();
  await render(UpdateTransaction, {
    props: {
      source: maintenanceSource({
        upgrade: preparedUpgrade(),
        version: {
          ...base.version!,
          managed_externally: true,
          managed_reason: 'the container image owns the executable',
        },
      }),
    },
  });

  await expect
    .element(page.getByText(/the container image owns the executable/))
    .toBeVisible();
  await expect
    .element(page.getByText(/pulling the new container image/))
    .toBeVisible();
  await expect
    .element(page.getByRole('button', { name: 'Apply update' }))
    .toBeDisabled();
});

const POSTGRES_ACK =
  'I confirm that the PostgreSQL databases outside this backup are backed up.';

test('asks for no PostgreSQL acknowledgement where the backup covers every database', async () => {
  await render(UpdateTransaction, {
    props: { source: maintenanceSource({ upgrade: preparedUpgrade() }) },
  });

  await expect
    .element(page.getByRole('button', { name: 'Apply update' }))
    .toBeEnabled();
  expect(page.getByRole('checkbox', { name: POSTGRES_ACK }).query()).toBeNull();
});

test('applies only after the PostgreSQL databases outside the backup are acknowledged', async () => {
  const apply = vi.fn(() => Promise.resolve());
  const base = maintenanceSource();
  await render(UpdateTransaction, {
    props: {
      source: maintenanceSource({
        apply,
        upgrade: preparedUpgrade(),
        version: { ...base.version!, postgres_backup_ack_required: true },
      }),
    },
  });

  const button = page.getByRole('button', { name: 'Apply update' });
  await expect.element(button).toBeDisabled();

  await page.getByRole('checkbox', { name: POSTGRES_ACK }).click();
  await button.click();

  expect(apply).toHaveBeenCalledWith(
    expect.objectContaining({ postgres_backup_ack: true }),
  );
});

test('cancels a scheduled update and dismisses a committed one', async () => {
  const cancel = vi.fn(() => Promise.resolve());
  const scheduled = await render(UpdateTransaction, {
    props: {
      source: maintenanceSource({
        cancel,
        upgrade: preparedUpgrade({ state: 'scheduled' }),
      }),
    },
  });
  await expect
    .element(page.getByRole('button', { name: 'Cancel update' }))
    .toBeInTheDocument();
  await scheduled.unmount();

  await render(UpdateTransaction, {
    props: {
      source: maintenanceSource({
        cancel,
        upgrade: preparedUpgrade({ state: 'committed' }),
      }),
    },
  });
  expect(
    page.getByRole('button', { name: 'Cancel update' }).all(),
  ).toHaveLength(0);
  await page.getByRole('button', { name: 'Dismiss' }).click();

  expect(cancel).toHaveBeenCalledOnce();
});
