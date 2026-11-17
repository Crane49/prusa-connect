import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { holdHost } from '../host';
import { settled } from '../test/facts';
import { fakePrinter, NAMESPACE, stubHost } from '../test/host.svelte';
import FailuresCard from './failures-card.svelte';
import KpiCard from './kpi-card.svelte';
import TrendCard from './trend-card.svelte';

const SIZE = { w: 6, h: 4 };

/** The figure a card shows under one label. */
const figure = (label: string) =>
  page
    .getByText(label, { exact: true })
    .elements()[0]
    ?.nextElementSibling?.textContent.trim();
const facts = [
  settled('a', 'finished', 'hook', 1, 1),
  settled('a', 'failed', 'hook', 2, 2),
  settled('b', 'cancelled', 'clip', 3, 3),
];
const fleet = () => [
  { ...fakePrinter('a'), name: 'MK4 by the door', tags: ['garage'] },
  fakePrinter('b'),
];

describe('the figures card', () => {
  it('counts the whole fleet, and links to the page', async () => {
    const { host } = stubHost(fleet(), facts);
    holdHost(host);
    await render(KpiCard, {
      config: {},
      title: '',
      size: SIZE,
      editing: false,
    });
    await expect.element(page.getByText('Fleet figures')).toBeInTheDocument();
    await expect
      .element(page.getByText('Whole fleet · last 7 days'))
      .toBeInTheDocument();
    await expect.poll(() => figure('Jobs')).toBe('3');
    await expect
      .element(page.getByRole('link', { name: 'Open statistics' }))
      .toHaveAttribute('href', `/m/${NAMESPACE}/fleet-stats`);
  });

  it('counts the printers a person picked, and opens the page on one of them', async () => {
    const { host } = stubHost(fleet(), facts);
    holdHost(host);
    await render(KpiCard, {
      config: { period: '30', printers: ['a'] },
      title: 'Door',
      size: SIZE,
      editing: false,
    });
    await expect.element(page.getByText('Door')).toBeInTheDocument();
    await expect
      .element(page.getByText('MK4 by the door · last 30 days'))
      .toBeInTheDocument();
    await expect.poll(() => figure('Jobs')).toBe('2');
    await expect
      .element(page.getByRole('link', { name: 'Open statistics' }))
      .toHaveAttribute('href', `/m/${NAMESPACE}/fleet-stats?days=30&printer=a`);
  });

  it('narrows to a tag, spelled however the person wrote it', async () => {
    const { host } = stubHost(fleet(), facts);
    holdHost(host);
    await render(KpiCard, {
      config: { tag: 'Garage' },
      title: '',
      size: SIZE,
      editing: false,
    });
    await expect
      .element(page.getByText('Tag Garage · last 7 days'))
      .toBeInTheDocument();
    await expect.poll(() => figure('Jobs')).toBe('2');
  });

  it('draws no link while the dashboard is being changed', async () => {
    const { host } = stubHost(fleet(), facts);
    holdHost(host);
    await render(KpiCard, { config: {}, title: '', size: SIZE, editing: true });
    await expect.element(page.getByText('Fleet figures')).toBeInTheDocument();
    expect(page.getByRole('link').elements()).toHaveLength(0);
  });
});

describe('the failures card', () => {
  it('lists the newest failures with their printers, as many as it was told', async () => {
    const { host } = stubHost(fleet(), facts);
    holdHost(host);
    await render(FailuresCard, {
      config: { limit: 1 },
      title: '',
      size: SIZE,
      editing: false,
    });
    await expect.element(page.getByText('hook')).toBeInTheDocument();
    await expect
      .element(page.getByText(/MK4 by the door · failed/))
      .toBeInTheDocument();
    expect(page.getByText('clip').elements()).toHaveLength(0);
  });

  it('says so when nothing failed', async () => {
    const { host } = stubHost(fleet(), [settled('a', 'finished')]);
    holdHost(host);
    await render(FailuresCard, {
      config: {},
      title: '',
      size: SIZE,
      editing: false,
    });
    await expect
      .element(page.getByText('No failures in this period'))
      .toBeInTheDocument();
  });
});

describe('the trend card', () => {
  it('names the metric it draws', async () => {
    const { host } = stubHost(fleet(), facts);
    holdHost(host);
    await render(TrendCard, {
      config: { period: '30', metric: 'hours' },
      title: '',
      size: SIZE,
      editing: false,
    });
    await expect
      .element(page.getByText('Print hours per day'))
      .toBeInTheDocument();
    await expect
      .element(page.getByText('Whole fleet · last 30 days'))
      .toBeInTheDocument();
  });
});
