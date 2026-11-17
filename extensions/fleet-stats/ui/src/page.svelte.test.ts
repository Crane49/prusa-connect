import { afterEach, describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { holdHost } from './host';
import Page from './page.svelte';
import { settled } from './test/facts';
import { fakePrinter, stubHost } from './test/host.svelte';

const start = window.location.href;
afterEach(() => {
  window.history.replaceState(null, '', start);
});

const facts = [
  settled('a', 'finished', 'hook', 1, 1),
  settled('a', 'failed', 'hook', 2, 2),
  settled('b', 'finished', 'clip', 3, 3),
];
const fleet = () => [fakePrinter('a'), fakePrinter('b')];

describe('the statistics page', () => {
  it('lists the files, most printed first', async () => {
    const { host } = stubHost(fleet(), facts);
    holdHost(host);
    await render(Page);
    await expect
      .element(page.getByRole('heading', { name: 'Files' }))
      .toBeInTheDocument();
    const rows = page
      .getByRole('row')
      .elements()
      .map((row) => row.textContent);
    const hook = rows.findIndex((text) => text.startsWith('hook'));
    const clip = rows.findIndex((text) => text.startsWith('clip'));
    expect(hook).toBeGreaterThan(-1);
    expect(hook).toBeLessThan(clip);
    await expect
      .element(page.getByRole('heading', { name: 'Trend' }))
      .toBeInTheDocument();
  });

  it('writes the period a person picks into the address', async () => {
    const { host } = stubHost(fleet(), facts);
    holdHost(host);
    await render(Page);
    await page.getByRole('radio', { name: '30 days' }).click();
    expect(new URLSearchParams(window.location.search).get('days')).toBe('30');
  });

  it('opens on the printer the address names', async () => {
    const url = new URL(start);
    url.search = '?printer=b';
    window.history.replaceState(null, '', url);
    const { host } = stubHost(fleet(), facts);
    holdHost(host);
    await render(Page);
    await expect
      .element(page.getByRole('cell', { name: 'clip' }))
      .toBeInTheDocument();
    expect(page.getByRole('cell', { name: 'hook' }).elements()).toHaveLength(0);
  });
});
