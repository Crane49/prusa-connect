import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { holdHost } from '../../host';
import { NAMESPACE, stubHost, TOKEN } from '../../test/fakes';
import PrusalinkRoute from './prusalink-route.svelte';

const ORIGIN = 'http://192.168.1.20:8080';

describe("the PrusaSlicer 2 section of a printer's page", () => {
  it('shows the address of this printer under the module', async () => {
    holdHost(stubHost().host);
    await render(PrusalinkRoute, { deviceId: 'mk4-01', origin: ORIGIN });

    await expect
      .element(page.getByLabelText('Hostname, IP or URL'))
      .toHaveValue(`${ORIGIN}/api/v1/modules/${NAMESPACE}/printers/mk4-01/`);
  });

  it('mints an operate credential named for no printer and shows it once', async () => {
    const { host, minted } = stubHost();
    holdHost(host);
    await render(PrusalinkRoute, { deviceId: 'mk4-01', origin: ORIGIN });

    await page.getByRole('button', { name: 'Create an API key' }).click();

    await expect.element(page.getByLabelText('API key')).toHaveValue(TOKEN);
    expect(minted).toHaveLength(1);
    expect(minted[0]?.scopes).toEqual(['operate']);
    expect(minted[0]?.name).toMatch(
      /^PrusaSlicer 2 Connector \(\d{4}-\d{2}-\d{2}\)$/,
    );
    expect(
      page.getByRole('button', { name: 'Create an API key' }).elements(),
    ).toHaveLength(0);
  });

  it('warns that the key crosses the network unencrypted over plain HTTP', async () => {
    holdHost(stubHost().host);
    await render(PrusalinkRoute, { deviceId: 'mk4-01', origin: ORIGIN });

    await page.getByRole('button', { name: 'Create an API key' }).click();

    await expect
      .element(page.getByText(/crosses your network unencrypted/))
      .toBeInTheDocument();
  });

  it('gives no such warning over HTTPS', async () => {
    holdHost(stubHost().host);
    await render(PrusalinkRoute, {
      deviceId: 'mk4-01',
      origin: 'https://connect.example:8443',
    });

    await page.getByRole('button', { name: 'Create an API key' }).click();

    await expect.element(page.getByLabelText('API key')).toHaveValue(TOKEN);
    expect(
      page.getByText(/crosses your network unencrypted/).elements(),
    ).toHaveLength(0);
  });

  it('shows the refusal and lets the person try again', async () => {
    const { host } = stubHost({
      refusal: Object.assign(new Error('denied'), {
        code: 'core.auth/ACCESS_DENIED',
      }),
    });
    holdHost(host);
    await render(PrusalinkRoute, { deviceId: 'mk4-01', origin: ORIGIN });

    await page.getByRole('button', { name: 'Create an API key' }).click();

    await expect.element(page.getByRole('alert')).toBeInTheDocument();
    await expect
      .element(page.getByRole('button', { name: 'Create an API key' }))
      .toBeEnabled();
  });

  it('turns Save to storage on for this printer in the first storage', async () => {
    const { host, kept } = stubHost();
    holdHost(host);
    await render(PrusalinkRoute, { deviceId: 'mk4-01', origin: ORIGIN });

    await page.getByRole('switch', { name: 'Save to storage' }).click();

    await expect
      .element(page.getByRole('switch', { name: 'Save to storage' }))
      .toBeChecked();
    expect(kept).toEqual([
      { printer: 'mk4-01', keep: { storage: 'local', folder: '' } },
    ]);
  });

  it('shows the storage the printer saves into as the module holds it', async () => {
    holdHost(stubHost({ keep: { storage: 'local', folder: 'prints' } }).host);
    await render(PrusalinkRoute, { deviceId: 'mk4-01', origin: ORIGIN });

    await expect
      .element(page.getByRole('switch', { name: 'Save to storage' }))
      .toBeChecked();
    await expect.element(page.getByText(/prints/)).toBeInTheDocument();
  });

  it('puts the switch back and says why when the module refuses', async () => {
    const { host } = stubHost({
      keepRefusal: Object.assign(new Error('denied'), {
        code: 'core.auth/ACCESS_DENIED',
      }),
    });
    holdHost(host);
    await render(PrusalinkRoute, { deviceId: 'mk4-01', origin: ORIGIN });

    await page.getByRole('switch', { name: 'Save to storage' }).click();

    await expect.element(page.getByRole('alert')).toBeInTheDocument();
    await expect
      .element(page.getByRole('switch', { name: 'Save to storage' }))
      .not.toBeChecked();
  });

  it('reads Save to storage once and holds still', async () => {
    const { host, reads } = stubHost();
    holdHost(host);
    await render(PrusalinkRoute, { deviceId: 'mk4-01', origin: ORIGIN });

    await expect
      .element(page.getByRole('switch', { name: 'Save to storage' }))
      .toBeInTheDocument();
    await new Promise((resolve) => setTimeout(resolve, 300));

    expect(reads()).toBe(1);
  });

  it('does not read Save to storage again when the host hands the same printer', async () => {
    const { host, reads } = stubHost();
    holdHost(host);
    const drawn = await render(PrusalinkRoute, {
      deviceId: 'mk4-01',
      origin: ORIGIN,
    });

    await expect
      .element(page.getByRole('switch', { name: 'Save to storage' }))
      .toBeInTheDocument();
    for (let report = 0; report < 5; report += 1) {
      await drawn.rerender({ deviceId: 'mk4-01', origin: ORIGIN });
    }
    await new Promise((resolve) => setTimeout(resolve, 300));

    expect(reads()).toBe(1);
  });

  it('says PrusaSlicer has not connected to a printer it never reached', async () => {
    holdHost(stubHost().host);
    await render(PrusalinkRoute, { deviceId: 'mk4-01', origin: ORIGIN });

    await expect
      .element(page.getByText('PrusaSlicer has not connected yet'))
      .toBeInTheDocument();
  });

  it('says how long ago PrusaSlicer last reached the printer', async () => {
    const at = new Date(Date.now() - 5 * 60_000 - 10_000).toISOString();
    holdHost(stubHost({ seenAt: at }).host);
    await render(PrusalinkRoute, { deviceId: 'mk4-01', origin: ORIGIN });

    await expect
      .element(page.getByText(/last seen 5 minutes ago/))
      .toBeInTheDocument();
    const line = page.getByTestId('seen').element().textContent;
    expect(line.replace(/\s+/g, ' ')).toContain(
      'PrusaSlicer connected, last seen 5 minutes ago',
    );
  });

  it('shows Save to storage read-only to a person who may not change it', async () => {
    holdHost(
      stubHost({
        keep: { storage: 'local', folder: 'prints' },
        mayChange: false,
      }).host,
    );
    await render(PrusalinkRoute, { deviceId: 'mk4-01', origin: ORIGIN });

    await expect
      .element(page.getByText('Uploads to this printer are saved to storage.'))
      .toBeInTheDocument();
    expect(
      page.getByRole('switch', { name: 'Save to storage' }).elements(),
    ).toHaveLength(0);
  });

  it('keeps a storage the person may not write to when they change the folder', async () => {
    const { host, kept } = stubHost({
      keep: { storage: 'nas', folder: 'old' },
    });
    holdHost(host);
    await render(PrusalinkRoute, { deviceId: 'mk4-01', origin: ORIGIN });

    await expect
      .element(page.getByText(/a storage you may not write to/))
      .toBeInTheDocument();
    await page.getByRole('button', { name: 'Choose folder' }).click();
    await page.getByRole('button', { name: 'Save here' }).click();

    expect(kept.at(-1)?.keep.storage).toBe('nas');
  });
});
