// The databases extensions left behind: listed by instance with where each
// is and why, and purged only after the person typed the instance's name.
import { describe, expect, it, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import {
  type LeftDatabase,
  LeftDatabases,
} from '../../stores/left-databases.svelte';
import LeftDatabasesSection from './left-databases.svelte';

const left: LeftDatabase = {
  instance: 'sink:notes',
  type: 'com.github.aux.acme.notes/notes',
  engine: 'sqlite',
  where: '/srv/module-data/notes/database.sqlite',
  left_at: '2026-10-21T09:30:00Z',
  why: 'removed',
  purge: 'delete',
};

describe('LeftDatabases', () => {
  it('draws nothing while nothing is left behind', async () => {
    const store = new LeftDatabases({
      listModuleDatabases: vi.fn().mockResolvedValue({ items: [] }),
      purgeModuleDatabase: vi.fn(),
    });
    const { container } = await render(LeftDatabasesSection, {
      props: { store },
    });
    await store.load();

    expect(container.querySelector('[data-part="left-databases"]')).toBeNull();
  });

  it('lists a database and purges it with the name the person typed', async () => {
    const purge = vi.fn().mockResolvedValue({
      items: [
        {
          instance: 'sink:notes',
          engine: 'sqlite',
          where: left.where,
          done: 'deleted',
        },
      ],
    });
    const store = new LeftDatabases({
      listModuleDatabases: vi
        .fn()
        .mockResolvedValueOnce({ items: [left] })
        .mockResolvedValue({ items: [] }),
      purgeModuleDatabase: purge,
    });
    await render(LeftDatabasesSection, { props: { store } });

    await expect
      .element(page.getByText(`A SQLite file at ${left.where}`))
      .toBeVisible();

    await page.getByRole('button', { name: 'Purge' }).click();
    await expect
      .element(
        page.getByText('This server deletes the SQLite file.', {
          exact: false,
        }),
      )
      .toBeVisible();

    await page.getByLabelText('Type sink:notes to confirm').fill('sink:notes');
    await page
      .getByRole('alertdialog')
      .getByRole('button', { name: 'Purge' })
      .click();

    expect(purge).toHaveBeenCalledWith({
      path: { kind: 'sink', name: 'notes' },
      query: { confirm: 'sink:notes' },
    });
    await expect
      .element(page.getByText(`Deleted: ${left.where}`))
      .toBeVisible();
  });
});
