import { describe, expect, it, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import type { SendsIndicatorRow } from '../../sends-indicator';
import PrinterFileTransfers from './printer-file-transfers.svelte';
import type { PrinterFileTransfer } from './types';

const transfer = (
  over: Partial<PrinterFileTransfer> = {},
): PrinterFileTransfer => ({
  transferId: 't-1',
  storage: 'local',
  source: 'models/vase.bgcode',
  path: 'usb/vase.bgcode',
  bytesTotal: 100,
  bytesSent: 40,
  state: 'sending',
  reason: undefined,
  ...over,
});

const upload = (over: Partial<SendsIndicatorRow> = {}): SendsIndicatorRow => ({
  id: 'job:1',
  deviceId: 'p1',
  printerName: 'Printer One',
  name: 'cube.bgcode',
  step: 'uploading',
  progress: 0.4,
  mine: true,
  startedBy: undefined,
  canCancel: true,
  stopping: false,
  ...over,
});

describe('PrinterFileTransfers', () => {
  it('draws nothing with no uploads and no transfers', async () => {
    await render(PrinterFileTransfers, { queue: [] });

    expect(page.getByText('cube.bgcode').elements()).toHaveLength(0);
    expect(page.getByRole('progressbar').elements()).toHaveLength(0);
  });

  it('draws an upload row above the transfers, with its own state, a named bar and a Cancel button', async () => {
    const onCancelUpload = vi.fn();
    await render(PrinterFileTransfers, {
      queue: [transfer()],
      uploads: [upload()],
      onCancelUpload,
    });

    const names = page
      .getByRole('listitem')
      .elements()
      .map((el) => el.textContent);
    expect(names[0]).toContain('cube.bgcode');
    expect(names[1]).toContain('vase.bgcode');

    await expect
      .element(page.getByText('Uploading to the server'))
      .toBeVisible();
    await expect
      .element(page.getByRole('progressbar', { name: 'Uploading cube.bgcode' }))
      .toHaveAttribute('aria-valuenow', '40');

    await page
      .getByRole('button', { name: 'Cancel the upload of cube.bgcode' })
      .click();
    expect(onCancelUpload).toHaveBeenCalledWith('job:1');
  });

  it('counts the upload rows in the empty check, and shows with no transfer queued', async () => {
    await render(PrinterFileTransfers, {
      queue: [],
      uploads: [upload()],
    });

    await expect.element(page.getByText('cube.bgcode')).toBeVisible();
  });
});
