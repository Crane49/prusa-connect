// The ports of an installed extension: what it holds now, and a save that
// sends only the ports moved away from the manifest's numbers.
import { describe, expect, it, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import ExtensionPorts from './extension-ports.svelte';

const syslog = {
  name: 'syslog',
  network: 'udp' as const,
  port: 13514,
  why: 'printer log lines',
};
const metrics = {
  name: 'metrics',
  network: 'udp' as const,
  port: 8514,
  why: 'printer metrics',
};

const field = (why: string) =>
  page.getByRole('spinbutton', { name: `UDP port for ${why}` });
const save = () => page.getByRole('button', { name: 'Save and allow ports' });

describe('ExtensionPorts', () => {
  it('shows each port at the number it listens on, and saves a moved one', async () => {
    const onsave = vi.fn().mockResolvedValue(true);
    await render(ExtensionPorts, {
      props: {
        asked: [syslog, metrics],
        listening: [{ ...syslog, port: 5514 }, metrics],
        busy: false,
        onsave,
      },
    });

    await expect.element(field('printer log lines')).toHaveValue(5514);
    await expect.element(page.getByText('Listening on 5514')).toBeVisible();
    await expect.element(save()).toBeDisabled();

    await field('printer metrics').fill('9514');
    await save().click();

    expect(onsave).toHaveBeenCalledWith({ syslog: 5514, metrics: 9514 });
  });

  it('allows ports left out at install, and refuses a number that is not a port', async () => {
    const onsave = vi.fn().mockResolvedValue(true);
    await render(ExtensionPorts, {
      props: { asked: [syslog], listening: [], busy: false, onsave },
    });

    await expect.element(page.getByText('Not allowed')).toBeVisible();
    await expect.element(save()).toBeEnabled();

    await field('printer log lines').fill('0');
    await expect
      .element(page.getByText('That is not a port from 1 to 65535.'))
      .toBeVisible();
    await expect.element(save()).toBeDisabled();

    await field('printer log lines').fill('13514');
    await save().click();
    expect(onsave).toHaveBeenCalledWith({});
  });

  it('says why the ports cannot be changed, and offers no save', async () => {
    await render(ExtensionPorts, {
      props: {
        asked: [syslog],
        listening: [syslog],
        locked: 'Locked for the test.',
        busy: false,
        onsave: vi.fn(),
      },
    });

    await expect.element(page.getByText('Locked for the test.')).toBeVisible();
    await expect.element(save()).not.toBeInTheDocument();
  });
});
