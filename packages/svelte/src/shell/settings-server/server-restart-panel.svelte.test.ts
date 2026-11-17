import { describe, expect, it, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import ServerRestartPanel from './server-restart-panel.svelte';
import type { ServerRestartPanelProps } from './types';

const props = (
  over: Partial<ServerRestartPanelProps> = {},
): ServerRestartPanelProps => ({
  safeMode: false,
  allowed: true,
  phase: 'idle' as const,
  onRestart: vi.fn(),
  ...over,
});

const safe = () => page.getByRole('button', { name: 'Restart in safe mode' });
const normal = () => page.getByRole('button', { name: 'Restart normally' });

describe('ServerRestartPanel', () => {
  it('offers only the safe restart on a normal run', async () => {
    await render(ServerRestartPanel, { props: props() });

    await expect.element(safe()).toBeVisible();
    expect(normal().elements()).toHaveLength(0);
  });

  it('offers only the normal restart in safe mode', async () => {
    const onRestart = vi.fn();
    await render(ServerRestartPanel, {
      props: props({ safeMode: true, onRestart }),
    });

    expect(safe().elements()).toHaveLength(0);
    await normal().click();

    expect(onRestart).toHaveBeenCalledWith('normal');
  });

  it('restarts into safe mode only after the confirmation', async () => {
    const onRestart = vi.fn();
    await render(ServerRestartPanel, { props: props({ onRestart }) });

    await safe().click();
    expect(onRestart).not.toHaveBeenCalled();
    await page
      .getByRole('alertdialog')
      .getByRole('button', { name: 'Restart in safe mode' })
      .click();

    expect(onRestart).toHaveBeenCalledWith('safe');
    await expect.element(page.getByRole('alertdialog')).not.toBeInTheDocument();
  });

  it.each([
    { allowed: false },
    { allowed: true, refusal: 'forbidden' as const },
  ])('shows the locked line and no control for %o', async (over) => {
    await render(ServerRestartPanel, { props: props(over) });

    await expect.element(page.getByText(/Only an organization/)).toBeVisible();
    expect(safe().elements()).toHaveLength(0);
  });

  it('offers no control while permission is unknown', async () => {
    await render(ServerRestartPanel, { props: props({ allowed: undefined }) });

    expect(safe().elements()).toHaveLength(0);
    expect(page.getByText(/Only an organization/).elements()).toHaveLength(0);
  });

  it('says an update is running on upgrade-busy', async () => {
    await render(ServerRestartPanel, {
      props: props({ refusal: 'upgrade-busy' as const }),
    });

    await expect.element(page.getByText(/is updating/)).toBeVisible();
  });

  it('blocks the click and says it waits while restarting', async () => {
    const onRestart = vi.fn();
    await render(ServerRestartPanel, {
      props: props({ safeMode: true, phase: 'waiting', onRestart }),
    });

    await expect
      .element(page.getByText('Waiting for the server'))
      .toBeVisible();
    await normal().click({ force: true });

    expect(onRestart).not.toHaveBeenCalled();
  });

  it('shows the fault of a failed request', async () => {
    await render(ServerRestartPanel, {
      props: props({
        phase: 'failed',
        error: { code: 'core.transport/NETWORK', params: {}, message: 'x' },
      }),
    });

    await expect.element(page.getByRole('alert')).toBeVisible();
  });

  it('says the permission could not be read and offers to read it again', async () => {
    const onRetryRead = vi.fn();
    await render(ServerRestartPanel, {
      props: props({
        allowed: undefined,
        readError: { status: 503, message: 'unavailable' },
        onRetryRead,
      }),
    });

    await expect
      .element(page.getByText('could not be read', { exact: false }))
      .toBeVisible();
    expect(safe().elements()).toHaveLength(0);
    await page.getByRole('button', { name: 'Try again' }).click();

    expect(onRetryRead).toHaveBeenCalledOnce();
  });
});
