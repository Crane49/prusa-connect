// A file page bookmarked before the address named its storage.
import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { appContext } from '../../test/app-context';
import { json, problem, type Routes } from '../../test/stub';
import type { AppContext } from '../screens';
import OldFileAddress from './old-file-address.svelte';

const storageOf = (name: string) => ({
  allowed_extensions: [],
  browse: true,
  display_name: name,
  download_url: true,
  move: true,
  name,
  ranges: true,
  write: true,
});

const serving = (names: readonly string[]): Routes => ({
  'GET /api/v1/storages': json(200, { items: names.map(storageOf) }),
  'GET /api/v1/storages/file/files': json(200, { items: [] }),
});

const opened = async (
  routes: Routes,
  search: string,
  before?: (ctx: AppContext) => Promise<unknown>,
) => {
  const went: { path: string; replace: boolean | undefined }[] = [];
  const ctx = appContext(routes, {
    search,
    go: (path, options) => {
      went.push({ path, replace: options?.replace });
    },
  });
  await before?.(ctx);
  await render(OldFileAddress, { props: { ctx } });

  return { went, ctx };
};

describe('an old file address', () => {
  // With one storage there is only one place the file can be.
  it('opens the file on the only storage', async () => {
    const { went } = await opened(
      serving(['local']),
      '?path=models%2Fcube.bgcode',
    );

    await expect
      .poll(() => went)
      .toEqual([
        { path: '/storage/local/file/models%2Fcube.bgcode', replace: true },
      ]);
  });

  // With two, the old address does not say which, so the reader chooses.
  it('opens the storage page when the server has more than one storage', async () => {
    const { went } = await opened(
      serving(['local', 'usb']),
      '?path=cube.bgcode',
    );

    await expect
      .poll(() => went)
      .toEqual([{ path: '/storage', replace: true }]);
  });

  // A path that names nothing is no file to open, so the reader chooses.
  it.each(['?path=', '?path=///'])(
    'opens the storage page for the empty path in %s',
    async (search) => {
      const { went } = await opened(serving(['local']), search);

      await expect
        .poll(() => went)
        .toEqual([{ path: '/storage', replace: true }]);
    },
  );

  // The browser rewrites a dot segment in an address, so the bar would name
  // another file than the page. No stored file has such a segment.
  it.each(['?path=..', '?path=.', '?path=models%2F..%2Fcube.bgcode'])(
    'opens the storage page for the dot segment in %s',
    async (search) => {
      const { went } = await opened(serving(['local']), search);

      await expect
        .poll(() => went)
        .toEqual([{ path: '/storage', replace: true }]);
    },
  );

  it('opens the storage page when the storage list cannot be read', async () => {
    const { went } = await opened(
      { 'GET /api/v1/storages': problem(500, 'core.api/UNEXPECTED') },
      '?path=cube.bgcode',
    );

    await expect
      .poll(() => went)
      .toEqual([{ path: '/storage', replace: true }]);
  });

  // A move refused on the storage page earlier leaves its error in the shared
  // store. It says nothing about this screen's own read of the storages.
  it('opens the file although an earlier move was refused', async () => {
    const { went } = await opened(
      {
        ...serving(['local']),
        'POST /api/v1/storages/local/moves': problem(
          409,
          'core.storage/EXISTS',
        ),
      },
      '?path=cube.bgcode',
      async (ctx) => {
        await ctx.storage.load();
        await ctx.storage.moveAll('local', [{ from: 'a', to: 'b' }]);
        expect(ctx.storage.error).toBeDefined();
      },
    );

    await expect
      .poll(() => went)
      .toEqual([{ path: '/storage/local/file/cube.bgcode', replace: true }]);
  });

  // Without a path it is the address of a storage that is named file.
  it('lists a storage named file when the address carries no path', async () => {
    const { went, ctx } = await opened(serving(['local', 'file']), '');

    await expect
      .element(page.getByRole('heading', { level: 1, name: 'Storage' }))
      .toBeInTheDocument();
    await expect.poll(() => ctx.storage.storage).toBe('file');
    expect(went).toEqual([]);
  });
});
