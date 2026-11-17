// The page of one file on this server. The address names the storage, so a
// server with two storages shows, and deletes, the file the reader asked for.
import { describe, expect, it, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { SlotRegistry } from '@prusa/connect-ui/slots';

import { appContext } from '../../test/app-context';
import { empty, json, type Routes } from '../../test/stub';
import WithSlots from '../../test/with-slots.svelte';
import FileDetail from './file-detail.svelte';

const storageOf = (name: string, displayName: string) => ({
  browse: true,
  display_name: displayName,
  download_url: true,
  move: true,
  name,
  ranges: true,
  write: true,
});

const bracket = {
  folder: false,
  modified: '2026-09-01T10:00:00Z',
  name: 'bracket.bgcode',
  path: 'models/bracket.bgcode',
  size: 2048,
};

const SERVED: Routes = {
  'GET /api/v1/storages': json(200, {
    items: [storageOf('local', 'Local'), storageOf('usb', 'USB drive')],
  }),
  'GET /api/v1/storages/usb/files': json(200, { items: [bracket] }),
  'GET /api/v1/storages/usb/stat': json(200, bracket),
};

const params = { storage: 'usb', path: 'models/bracket.bgcode' };

describe('the file page', () => {
  it('shows the file from the storage the address names', async () => {
    const ctx = appContext(SERVED, { params });

    await render(FileDetail, { props: { ctx } });

    await expect
      .element(page.getByRole('heading', { level: 1, name: /acket.bgcode/ }))
      .toBeInTheDocument();
    await expect
      .element(page.getByRole('link', { name: 'USB drive' }))
      .toHaveAttribute('href', '/storage/usb');
  });

  // The folder crumb returns to the folder the file sits in, which is where
  // the reader came from.
  it('leads the folder crumb to the folder and not to the root', async () => {
    const ctx = appContext(SERVED, { params });

    await render(FileDetail, { props: { ctx } });

    await expect
      .element(page.getByRole('link', { name: 'models' }))
      .toHaveAttribute('href', '/storage/usb/folder/models');
  });

  it('deletes the file from the storage the address names', async () => {
    const deleted: string[] = [];
    const went: string[] = [];
    const ctx = appContext(
      {
        ...SERVED,
        'DELETE /api/v1/storages/usb/files': (call) => {
          deleted.push(call.path);

          return empty(204);
        },
      },
      {
        params,
        go: (path) => {
          went.push(path);
        },
      },
    );

    await render(FileDetail, { props: { ctx } });
    await page.getByRole('button', { name: /Actions for/ }).click();
    await page.getByRole('menuitem', { name: 'Delete' }).click();
    await page
      .getByRole('alertdialog')
      .getByRole('button', { name: 'Delete' })
      .click();

    await expect.poll(() => deleted).toEqual(['/api/v1/storages/usb/files']);
    await expect.poll(() => went).toEqual(['/storage/usb/folder/models']);
  });

  it('offers a Download item that points at the storage content route', async () => {
    const ctx = appContext(SERVED, { params });

    await render(FileDetail, { props: { ctx } });
    await page.getByRole('button', { name: /Actions for/ }).click();

    await expect
      .element(page.getByRole('menuitem', { name: 'Download' }))
      .toHaveAttribute(
        'href',
        expect.stringMatching(
          /\/api\/v1\/storages\/usb\/content\?path=models%2Fbracket\.bgcode$/,
        ),
      );
  });

  // A file a printer takes as it is has a Print button, which opens the
  // print dialog on the file, as a row of the listing does.
  it('opens the print dialog from the Print button', async () => {
    const ctx = appContext(
      {
        ...SERVED,
        'GET /api/v1/devices': json(200, {
          items: [
            {
              id: 'a',
              name: 'Printer a',
              kind: 'printer',
              model: 'MK4S',
              connected: true,
              labels: {},
              capabilities: ['core.command.start_print'],
              state: { '/state': 'IDLE' },
            },
          ],
        }),
      },
      { params },
    );

    await render(FileDetail, { props: { ctx } });
    await page.getByRole('button', { name: 'Print', exact: true }).click();

    await expect
      .element(page.getByRole('dialog').getByText('bracket.bgcode').first())
      .toBeVisible();
  });

  // A server with no printer has nothing to print on.
  it('offers no Print button where this server holds no printer', async () => {
    const ctx = appContext(
      { ...SERVED, 'GET /api/v1/devices': json(200, { items: [] }) },
      { params },
    );

    await render(FileDetail, { props: { ctx } });

    await expect
      .element(page.getByRole('heading', { level: 1, name: /acket.bgcode/ }))
      .toBeInTheDocument();
    expect(
      page.getByRole('button', { name: 'Print', exact: true }).query(),
    ).toBeNull();
  });

  // A module's file-action shares the file's own menu, so the page shows one
  // menu and not two, and the module runs with the file's reference.
  it('runs a module file action from the file menu with the file reference', async () => {
    const registry = new SlotRegistry();
    registry.declare('test.later', {
      file_action: [{ id: 'later', title: 'Print later', destructive: false }],
    });
    const run = vi.fn(() => Promise.resolve());
    registry.registerAction('test.later', 'file-action', 'later', run);
    const ctx = appContext(SERVED, { params });

    await render(WithSlots, { props: { registry, screen: FileDetail, ctx } });
    await page.getByRole('button', { name: /Actions for/ }).click();

    await expect
      .element(page.getByRole('menuitem', { name: 'Delete' }))
      .toBeInTheDocument();
    await page.getByRole('menuitem', { name: 'Print later' }).click();

    expect(run).toHaveBeenCalledWith('usb:models/bracket.bgcode');
  });

  // An operator hides a module's file action (ADR-20261021-3dp); the file's own
  // items stay in the menu.
  it('leaves a hidden module file action out of the file menu', async () => {
    const registry = new SlotRegistry();
    registry.declare(
      'test.later',
      {
        file_action: [
          { id: 'plan', title: 'Plan a print', destructive: false },
        ],
      },
      ['file_action/plan'],
    );
    registry.registerAction('test.later', 'file-action', 'plan', vi.fn());
    const ctx = appContext(SERVED, { params });

    await render(WithSlots, { props: { registry, screen: FileDetail, ctx } });
    await page.getByRole('button', { name: /Actions for/ }).click();

    await expect
      .element(page.getByRole('menuitem', { name: 'Delete' }))
      .toBeInTheDocument();
    expect(
      page.getByRole('menuitem', { name: 'Plan a print' }).query(),
    ).toBeNull();
  });
});
