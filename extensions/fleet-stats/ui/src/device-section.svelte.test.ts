import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import DeviceSection from './device-section.svelte';
import { holdHost } from './host';
import { settled } from './test/facts';
import { fakePrinter, NAMESPACE, stubHost } from './test/host.svelte';

describe("the printer's section", () => {
  it('opens the page on this printer, and lists its failures', async () => {
    const { host } = stubHost(
      [fakePrinter('a'), fakePrinter('b')],
      [settled('a', 'failed', 'hook'), settled('b', 'failed', 'clip')],
    );
    holdHost(host);
    await render(DeviceSection, { deviceId: 'a' });
    await expect
      .element(page.getByRole('link', { name: 'Open statistics' }))
      .toHaveAttribute('href', `/m/${NAMESPACE}/fleet-stats?printer=a`);
    await expect.element(page.getByText('hook')).toBeInTheDocument();
    expect(page.getByText('clip').elements()).toHaveLength(0);
  });
});
