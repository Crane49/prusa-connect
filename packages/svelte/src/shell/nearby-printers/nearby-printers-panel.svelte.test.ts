import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import {
  type DiscoveryClient,
  type NearbyPrinter,
  NearbyPrinters,
} from '../../data';
import { StubApiError } from '../../data/stubs';
import { expectNoViolations } from '../../test/axe';
import NearbyPrintersPanel from './nearby-printers-panel.svelte';

const refuse = (): never => {
  throw new Error('this fake answers no visibility');
};

/** The real store over a fake client, so the panel is tested with what ships. */
const storeOver = (
  answer: DiscoveryClient['listNearbyPrinters'],
): NearbyPrinters =>
  new NearbyPrinters({
    listNearbyPrinters: answer,
    getDiscoveryVisibility: refuse,
    setDiscoveryVisibility: refuse,
  });

const DESK: NearbyPrinter = {
  address: '192.168.1.20',
  name: 'Desk MK4',
  port: 80,
  services: ['_prusalink._tcp'],
  tls: false,
  url: 'http://192.168.1.20:80/',
};

const LAB: NearbyPrinter = {
  address: '192.168.1.21',
  name: 'Lab XL',
  port: 80,
  services: ['_prusalink._tcp'],
  tls: false,
  url: 'http://192.168.1.21:80/',
  device_id: 'dev-lab',
};

const OTHER_SERVER: NearbyPrinter = {
  address: '192.168.1.30',
  name: 'Office Prusa Connect',
  port: 8080,
  services: ['_prusa-connect._tcp', '_prusalink._tcp'],
  tls: false,
  url: 'http://192.168.1.30:8080/',
};

describe('NearbyPrintersPanel', () => {
  it('links each printer to its own page and to the guide that adds it', async () => {
    const store = storeOver(() => Promise.resolve({ items: [DESK] }));
    await store.load();

    const { container } = await render(NearbyPrintersPanel, {
      props: { source: store, addHref: '/printers/add' },
    });

    const link = page.getByRole('link', { name: 'Desk MK4', exact: true });
    await expect
      .element(link)
      .toHaveAttribute('href', 'http://192.168.1.20:80/');
    await expect.element(page.getByText('192.168.1.20, port 80')).toBeVisible();
    const add = page.getByRole('link', { name: 'Add printer', exact: true });
    await expect.element(add).toHaveAttribute('href', '/printers/add');
    await expect.element(add).toHaveAccessibleDescription('Desk MK4');
    await expectNoViolations(container);
  });

  it('writes a port as it is, without grouping its digits', async () => {
    const store = storeOver(() =>
      Promise.resolve({ items: [{ ...DESK, port: 8080 }] }),
    );
    await store.load();

    await render(NearbyPrintersPanel, { props: { source: store } });

    await expect
      .element(page.getByText('192.168.1.20, port 8080'))
      .toBeVisible();
  });

  // A printer the server names is already on the page above the panel.
  it('leaves out a printer that is connected to this server', async () => {
    const store = storeOver(() => Promise.resolve({ items: [DESK, LAB] }));
    await store.load();

    await render(NearbyPrintersPanel, {
      props: { source: store, addHref: '/printers/add' },
    });

    await expect
      .element(page.getByRole('link', { name: 'Desk MK4', exact: true }))
      .toBeVisible();
    await expect.element(page.getByText('Lab XL')).not.toBeInTheDocument();
  });

  // Another Prusa Connect server is not a printer to add here.
  it('leaves out another Prusa Connect server', async () => {
    const store = storeOver(() =>
      Promise.resolve({ items: [OTHER_SERVER, DESK] }),
    );
    await store.load();

    await render(NearbyPrintersPanel, {
      props: { source: store, addHref: '/printers/add' },
    });

    await expect
      .element(page.getByRole('link', { name: 'Desk MK4', exact: true }))
      .toBeVisible();
    await expect
      .element(page.getByText('Office Prusa Connect'))
      .not.toBeInTheDocument();
  });

  it('draws nothing when it hears only another server', async () => {
    const store = storeOver(() => Promise.resolve({ items: [OTHER_SERVER] }));
    await store.load();

    const { container } = await render(NearbyPrintersPanel, {
      props: { source: store },
    });

    expect(container.querySelector('[data-slot="nearby-printers"]')).toBeNull();
  });

  it('offers no way to add a printer when the host gives no guide', async () => {
    const store = storeOver(() => Promise.resolve({ items: [DESK] }));
    await store.load();

    await render(NearbyPrintersPanel, { props: { source: store } });

    await expect
      .element(page.getByRole('link', { name: 'Desk MK4', exact: true }))
      .toBeVisible();
    await expect
      .element(page.getByRole('link', { name: 'Add printer', exact: true }))
      .not.toBeInTheDocument();
  });

  // The panel is a hint, so it says nothing until it has something to say.
  it('draws nothing when every printer it hears is already here', async () => {
    const store = storeOver(() => Promise.resolve({ items: [LAB] }));
    await store.load();

    const { container } = await render(NearbyPrintersPanel, {
      props: { source: store },
    });

    expect(container.querySelector('[data-slot="nearby-printers"]')).toBeNull();
  });

  it('draws nothing while the first read is in flight', async () => {
    const store = storeOver(() => new Promise(() => undefined));
    void store.load();

    const { container } = await render(NearbyPrintersPanel, {
      props: { source: store },
    });

    expect(container.querySelector('[data-slot="nearby-printers"]')).toBeNull();
  });

  // A person who may not see the list is not shown a refusal on a page
  // they came to for something else.
  it('draws nothing for a person who may not see the list', async () => {
    const store = storeOver(() =>
      Promise.reject(new StubApiError('core.auth/ACCESS_DENIED', 403)),
    );
    await store.load();

    const { container } = await render(NearbyPrintersPanel, {
      props: { source: store },
    });

    expect(container.querySelector('[data-slot="nearby-printers"]')).toBeNull();
  });

  it('shows any other refusal by its code', async () => {
    const store = storeOver(() =>
      Promise.reject(new StubApiError('core.internal/UNEXPECTED', 500)),
    );
    await store.load();

    await render(NearbyPrintersPanel, { props: { source: store } });

    await expect.element(page.getByRole('alert')).toBeVisible();
    await expect
      .element(page.getByText('core.internal/UNEXPECTED'))
      .toBeVisible();
  });

  // Two names on one address is a server that answers under two titles, or
  // two servers behind one address. Both are drawn.
  it('draws two printers that share one address', async () => {
    const store = storeOver(() =>
      Promise.resolve({ items: [DESK, { ...DESK, name: 'Desk MK4 (2)' }] }),
    );
    await store.load();

    await render(NearbyPrintersPanel, { props: { source: store } });

    await expect
      .element(page.getByRole('link', { name: 'Desk MK4 (2)' }))
      .toBeVisible();
    await expect
      .element(page.getByRole('link', { name: 'Desk MK4', exact: true }))
      .toBeVisible();
  });
});
