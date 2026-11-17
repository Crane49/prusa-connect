import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import DeviceCard from './device-card.svelte';
import { holdHost } from './host';
import { settled } from './test/facts';
import { fakePrinter, stubHost } from './test/host.svelte';

describe('the device card line', () => {
  it('says nothing for a printer with no jobs in the period', async () => {
    const { host } = stubHost(
      [fakePrinter('a'), fakePrinter('b')],
      [settled('b', 'finished')],
    );
    holdHost(host);
    const { container } = await render(DeviceCard, { deviceId: 'a' });
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(container.textContent.trim()).toBe('');
  });

  it('gives success and job count for a printer that printed', async () => {
    const { host } = stubHost([fakePrinter('b')], [settled('b', 'finished')]);
    holdHost(host);
    await render(DeviceCard, { deviceId: 'b' });
    await expect
      .element(page.getByText(/100\s?% · 1 job · 7 d/))
      .toBeInTheDocument();
  });
});
