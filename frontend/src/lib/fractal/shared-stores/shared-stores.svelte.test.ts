import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { formatBytes } from '@prusa/connect-ui/i18n';

import {
  FractalStores,
  type FractalStoresClient,
  type StoreEntry,
} from '../../stores/fractal-stores.svelte';
import SharedStores from './shared-stores.svelte';

const storeOf = (
  node: string,
  instance: string,
  stale = false,
  rootHash: string | null = `root-${node}`,
) => ({
  node,
  interface: 'ContentStore1',
  instance,
  status: stale ? ('STALE' as const) : ('OK' as const),
  updated_at: 1_793_700_000_000,
  props: { rootHash },
});

const listings: Record<string, StoreEntry[]> = {
  'root-hq': [
    { name: 'projects', kind: 'directory', size: 0, identity: 'projects' },
  ],
  projects: [
    { name: 'benchy.bgcode', kind: 'file', size: 2048, identity: 'benchy' },
  ],
  'root-hq/brno/zabovresky': [
    { name: 'jigs', kind: 'directory', size: 0, identity: 'jigs' },
  ],
};

const over = (items: ReturnType<typeof storeOf>[]): FractalStores => {
  const client: FractalStoresClient = {
    listFractalStores: () =>
      Promise.resolve({
        node: 'hq/brno',
        items,
        total: items.length,
        truncated: false,
      }),
    readFractalStoreListing: ({ path }) =>
      Promise.resolve({ ...path, entries: listings[path.identity] ?? [] }),
  };

  return new FractalStores(client);
};

const tree = () =>
  over([storeOf('hq', 'local'), storeOf('hq/brno/zabovresky', 'local', true)]);

describe('SharedStores', () => {
  it('says where every row is: server, storage and folder', async () => {
    await render(SharedStores, { props: { stores: tree() } });

    await expect
      .element(page.getByRole('heading', { name: 'Shared by other servers' }))
      .toBeVisible();
    await expect
      .element(page.getByRole('button', { name: 'projects' }))
      .toBeVisible();
    await expect
      .element(page.getByText('hq · local · /', { exact: true }))
      .toBeVisible();
  });

  it('opens a folder in place and goes back up the trail', async () => {
    await render(SharedStores, { props: { stores: tree() } });

    await page.getByRole('button', { name: 'projects' }).click();

    await expect.element(page.getByText('benchy.bgcode')).toBeVisible();
    await expect.element(page.getByText(formatBytes(2048))).toBeVisible();
    await expect.element(page.getByText('hq · local · projects')).toBeVisible();

    await page.getByRole('button', { name: 'All servers' }).click();

    await expect
      .element(page.getByRole('button', { name: 'jigs' }))
      .toBeVisible();
  });

  it('greys out a server not heard from, with when it last was', async () => {
    await render(SharedStores, { props: { stores: tree() } });

    await expect
      .element(page.getByText(/No report from hq\/brno\/zabovresky since/))
      .toBeVisible();
    await expect
      .element(page.getByRole('row').filter({ hasText: 'jigs' }))
      .toHaveAttribute('data-state', 'stale');
  });

  it('narrows the top to one server', async () => {
    await render(SharedStores, { props: { stores: tree() } });

    await page.getByRole('combobox', { name: 'Server' }).click();
    await page.getByRole('option', { name: 'hq', exact: true }).click();

    await expect
      .element(page.getByRole('button', { name: 'jigs' }))
      .not.toBeInTheDocument();
    await expect
      .element(page.getByRole('button', { name: 'projects' }))
      .toBeVisible();
  });

  it('says a store that has no listing yet is not empty', async () => {
    await render(SharedStores, {
      props: {
        stores: over([storeOf('hq', 'usb', false, null)]),
      },
    });

    await expect
      .element(page.getByRole('heading', { name: 'No shared folders to show' }))
      .toBeVisible();
    await expect
      .element(page.getByText('hq has not listed the folders of usb yet.'))
      .toBeVisible();
    expect(page.getByText('This folder is empty').query()).toBeNull();
  });

  it('says nothing is shared, which is not a failure', async () => {
    await render(SharedStores, { props: { stores: over([]) } });

    await expect
      .element(page.getByText('Nothing shared with this server'))
      .toBeVisible();
  });
});
