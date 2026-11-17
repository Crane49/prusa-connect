// Every screen that draws a printer icon reads the device-model vocabulary, so
// an icon for a model a driver declared has the declared name to show
// (ADR-20261016-qrp). The icon only looks the name up; a screen that never asked for
// the vocabulary leaves every declared printer unrecognised on it.
import { describe, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';

import { appContext } from '../../test/app-context';
import type { AppContext } from '../screens';
import Dashboard from './dashboard.svelte';
import FileDetail from './file-detail.svelte';
import JobDetail from './job-detail.svelte';
import PrinterDetail from './printer-detail.svelte';
import Printers from './printers.svelte';

const reading = (over: Partial<AppContext> = {}) => {
  const ctx = appContext({}, over);
  const read = vi.spyOn(ctx.deviceModels, 'read');

  return { ctx, read };
};

describe('the screens that draw a printer icon', () => {
  it('read the vocabulary on the printers page', async () => {
    const { ctx, read } = reading();
    await render(Printers, { props: { ctx } });

    await expect.poll(() => read.mock.calls.length).toBeGreaterThan(0);
  });

  it('read the vocabulary on a printer', async () => {
    const { ctx, read } = reading({ params: { id: 'a' } });
    await render(PrinterDetail, { props: { ctx } });

    await expect.poll(() => read.mock.calls.length).toBeGreaterThan(0);
  });

  // The dashboard draws its frame from a chunk of its own. The test loads the
  // chunk first and hands it in, so the poll waits on the read and not on the
  // import.
  it('read the vocabulary on a dashboard', async () => {
    const { ctx, read } = reading();
    const lib = await import('@prusa/connect-ui/dashboard');
    await render(Dashboard, {
      props: { ctx, load: () => Promise.resolve(lib) },
    });

    await expect.poll(() => read.mock.calls.length).toBeGreaterThan(0);
  });

  it('read the vocabulary on a print', async () => {
    const { ctx, read } = reading({ params: { id: 'job-1' } });
    await render(JobDetail, { props: { ctx } });

    await expect.poll(() => read.mock.calls.length).toBeGreaterThan(0);
  });

  it('read the vocabulary on a file', async () => {
    const { ctx, read } = reading({
      params: { storage: 'usb', path: 'models/bracket.bgcode' },
    });
    await render(FileDetail, { props: { ctx } });

    await expect.poll(() => read.mock.calls.length).toBeGreaterThan(0);
  });
});
