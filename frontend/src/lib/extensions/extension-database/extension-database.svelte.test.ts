// Where a module's own database goes: the places offered, the one an update
// keeps, and a connection string that is not a Postgres address.
import { describe, expect, it, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import ExtensionDatabase from './extension-database.svelte';

const radio = (name: string) => page.getByRole('radio', { name });

describe('ExtensionDatabase', () => {
  it('offers three places on a first install and reports the one picked', async () => {
    const onpick = vi.fn();
    await render(ExtensionDatabase, {
      props: { mode: 'sqlite', update: false, onpick },
    });

    await expect.element(radio('A file on this server (SQLite)')).toBeChecked();
    await expect
      .element(page.getByRole('radio', { name: 'Its current database' }))
      .not.toBeInTheDocument();

    await radio("A new database on this server's Postgres").click();
    expect(onpick).toHaveBeenCalledWith('provision');
  });

  it('keeps the database an installed module has, and says where it is', async () => {
    await render(ExtensionDatabase, {
      props: {
        mode: 'keep',
        update: true,
        current: {
          engine: 'postgres',
          provisioned: true,
          server: 'db.lan:5432/module_notes',
        },
        onpick: vi.fn(),
      },
    });

    await expect.element(radio('Its current database')).toBeChecked();
    await expect
      .element(page.getByText('db.lan:5432/module_notes', { exact: false }))
      .toBeVisible();
  });

  it('asks for a Postgres address and refuses one it cannot use', async () => {
    await render(ExtensionDatabase, {
      props: {
        mode: 'postgres',
        dsn: 'host=db',
        update: false,
        onpick: vi.fn(),
      },
    });

    await expect
      .element(
        page.getByText(
          'Write a postgres:// address with a host and a database name.',
        ),
      )
      .toBeVisible();

    await page
      .getByLabelText('Connection string', { exact: false })
      .fill('postgres://notes:pw@db.lan/notes');
    await expect
      .element(
        page.getByText(
          'Write a postgres:// address with a host and a database name.',
        ),
      )
      .not.toBeInTheDocument();
  });
});
