// The page of one print. Its file link leads to the file on this server that
// was sent to the printer, and a file carried over on a drive has no link.
import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { appContext } from '../../test/app-context';
import { json, type Routes } from '../../test/stub';
import JobDetail from './job-detail.svelte';

const JOB_ID = '0199e3a4-5bb5-7aac-8a2c-9c3982e6373f';

const job = {
  device_id: 'mk4',
  device_job_id: '412',
  display_name: 'bracket.bgcode',
  ended: '2026-09-01T09:00:00Z',
  id: JOB_ID,
  path: 'usb/bracket.bgcode',
  progress: 100,
  started: '2026-09-01T08:00:00Z',
  state: 'FINISHED',
  time_remaining_ms: 0,
};

const served = (transfers: unknown[]): Routes => ({
  'GET /api/v1/devices': json(200, {
    items: [
      { id: 'mk4', name: 'MK4S', kind: 'printer', model: 'MK4S', labels: {} },
    ],
  }),
  [`GET /api/v1/jobs/${JOB_ID}`]: json(200, job),
  'GET /api/v1/devices/mk4/transfers': json(200, { items: transfers }),
});

const sentFrom = (source: string, startedAt: string, state = 'done') => ({
  bytes_sent: 2048,
  bytes_total: 2048,
  path: 'usb/bracket.bgcode',
  source,
  started_at: startedAt,
  // The sends here are short, so each one ends the moment it starts.
  ended_at: startedAt,
  state,
  storage: 'local',
  transfer_id: `t-${source}-${startedAt}`,
});

const sent = sentFrom('models/bracket.bgcode', '2026-09-01T07:00:00Z');

describe('the print page', () => {
  // The printer is named by the print, so nothing about it is read before the
  // print is: a transfer read for no printer is a request to an empty id.
  it('reads no transfers before it knows the printer', async () => {
    let unnamed = 0;
    const ctx = appContext(
      {
        ...served([sent]),
        'GET /api/v1/devices//transfers': () => {
          unnamed += 1;

          return json(200, { items: [] });
        },
      },
      { params: { id: JOB_ID } },
    );

    await render(JobDetail, { props: { ctx } });

    await expect
      .element(page.getByRole('link', { name: 'Open this file' }))
      .toBeVisible();
    expect(unnamed).toBe(0);
  });

  it('leads to the file on this server that was sent to the printer', async () => {
    const ctx = appContext(served([sent]), { params: { id: JOB_ID } });

    await render(JobDetail, { props: { ctx } });

    await expect
      .element(page.getByRole('link', { name: 'Open this file' }))
      .toHaveAttribute('href', '/storage/local/file/models%2Fbracket.bgcode');
  });

  // Without a transfer this server does not know where the file came from,
  // and a link to the printer's folder would not be the file.
  it('offers no file link for a file this server did not send', async () => {
    const ctx = appContext(served([]), { params: { id: JOB_ID } });

    await render(JobDetail, { props: { ctx } });

    await expect
      .element(page.getByRole('heading', { level: 1, name: /acket/ }))
      .toBeInTheDocument();
    expect(
      page.getByRole('link', { name: 'Open this file' }).elements(),
    ).toHaveLength(0);
  });

  // The page reads the printer's transfers through the store the app shares,
  // so a second visit asks the server again only when the store says so.
  it('reads the transfers through the shared store', async () => {
    const ctx = appContext(served([sent]), { params: { id: JOB_ID } });

    await render(JobDetail, { props: { ctx } });

    await expect.poll(() => ctx.transfers('mk4').loaded).toBe(true);
  });

  // The printer keeps the file that arrived last before the print started.
  // A send after the print, and a send that failed, are not that file.
  it.each([
    ['newest first', false],
    ['oldest first', true],
  ])(
    'leads to the last file sent before the print, listed %s',
    async (_order, reversed) => {
      const rows = [
        sentFrom('late/bracket.bgcode', '2026-09-01T09:00:00Z'),
        sentFrom('broken/bracket.bgcode', '2026-09-01T07:45:00Z', 'failed'),
        sentFrom('y/bracket.bgcode', '2026-09-01T07:30:00Z'),
        sentFrom('x/bracket.bgcode', '2026-09-01T07:00:00Z'),
      ];
      const ctx = appContext(served(reversed ? rows.reverse() : rows), {
        params: { id: JOB_ID },
      });

      await render(JobDetail, { props: { ctx } });

      await expect
        .element(page.getByRole('link', { name: 'Open this file' }))
        .toHaveAttribute('href', '/storage/local/file/y%2Fbracket.bgcode');
    },
  );
});
