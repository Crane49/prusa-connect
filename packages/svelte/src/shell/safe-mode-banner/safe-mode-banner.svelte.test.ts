import { describe, expect, it, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import SafeModeBanner from './safe-mode-banner.svelte';

const base = {
  since: '2026-10-30T09:12:00.000Z',
  restarting: false,
  timedOut: false,
};

const button = () => page.getByRole('button', { name: 'Restart normally' });

describe('SafeModeBanner', () => {
  it('offers the restart to a person who may and calls back on click', async () => {
    const onRestartNormally = vi.fn();
    await render(SafeModeBanner, {
      props: { ...base, allowed: true, onRestartNormally },
    });

    await button().click();

    expect(onRestartNormally).toHaveBeenCalledOnce();
  });

  it.each([false, undefined])(
    'offers no button when allowed is %s',
    async (allowed) => {
      await render(SafeModeBanner, {
        props: { ...base, allowed, onRestartNormally: vi.fn() },
      });

      expect(button().elements()).toHaveLength(0);
    },
  );

  it('does not call back while the restart runs', async () => {
    const onRestartNormally = vi.fn();
    await render(SafeModeBanner, {
      props: { ...base, allowed: true, restarting: true, onRestartNormally },
    });

    const busy = page.getByRole('button', { name: 'Restarting…' });
    await expect.element(busy).toHaveAttribute('aria-disabled', 'true');
    await busy.click({ force: true });

    expect(onRestartNormally).not.toHaveBeenCalled();
  });

  it('says the server has not come back after a timeout', async () => {
    await render(SafeModeBanner, {
      props: {
        ...base,
        allowed: true,
        timedOut: true,
        onRestartNormally: vi.fn(),
      },
    });

    await expect.element(page.getByText(/has not come back/)).toBeVisible();
  });

  it('words a refusal and drops the button on a 403', async () => {
    await render(SafeModeBanner, {
      props: {
        ...base,
        allowed: true,
        refusal: 'forbidden',
        onRestartNormally: vi.fn(),
      },
    });

    await expect.element(page.getByText(/Only an organization/)).toBeVisible();
    expect(button().elements()).toHaveLength(0);
  });

  it('words an update in progress and keeps the button', async () => {
    await render(SafeModeBanner, {
      props: {
        ...base,
        allowed: true,
        refusal: 'upgrade-busy',
        onRestartNormally: vi.fn(),
      },
    });

    await expect.element(page.getByText(/is updating/)).toBeVisible();
    await expect.element(button()).toBeVisible();
  });
});
