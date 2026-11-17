import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import TransferLog from './transfer-log.svelte';

const CANCELLED = {
  transferId: 't1',
  storage: 'local',
  source: 'bracket.gcode',
  path: 'usb/bracket.gcode',
  bytesTotal: 31_457_346,
  bytesSent: 50_242,
  state: 'cancelled',
  reason: 'the caller stopped waiting',
  startedAt: '2026-09-23T10:00:00.000Z',
  endedAt: '2026-09-23T10:00:02.000Z',
};

describe('TransferLog', () => {
  // A send that stopped part way is what a person comes here to explain: the
  // file, how it ended, how far it got and why.
  it('draws a cancelled transfer with its state, its bytes and its reason', async () => {
    await render(TransferLog, {
      props: { source: { rows: [CANCELLED], loaded: true, error: undefined } },
    });

    await expect.element(page.getByText('bracket.gcode')).toBeVisible();
    await expect.element(page.getByText('Cancelled')).toBeVisible();
    await expect
      .element(page.getByText('the caller stopped waiting'))
      .toBeVisible();
    await expect.element(page.getByText(/ of /u)).toBeVisible();
  });

  it('says nothing was sent where the printer has no transfer', async () => {
    await render(TransferLog, {
      props: { source: { rows: [], loaded: true, error: undefined } },
    });

    await expect
      .element(page.getByText('No file was sent to this printer'))
      .toBeVisible();
  });

  // The store keeps the rows of an earlier read when a later one fails.
  it('draws a failed read beside the rows it already had', async () => {
    await render(TransferLog, {
      props: {
        source: {
          rows: [CANCELLED],
          loaded: true,
          error: {
            code: 'core.transport/NETWORK',
            status: undefined,
            params: {},
            message: 'the server could not be reached',
            traceId: undefined,
          },
        },
      },
    });

    await expect.element(page.getByRole('alert')).toBeVisible();
    await expect.element(page.getByText('bracket.gcode')).toBeVisible();
  });

  // A state a later build adds is shown as the server spelled it.
  it('draws a state it does not know as the server spelled it', async () => {
    await render(TransferLog, {
      props: {
        source: {
          rows: [{ ...CANCELLED, state: 'paused', reason: undefined }],
          loaded: true,
          error: undefined,
        },
      },
    });

    await expect.element(page.getByText('paused')).toBeVisible();
  });
});
