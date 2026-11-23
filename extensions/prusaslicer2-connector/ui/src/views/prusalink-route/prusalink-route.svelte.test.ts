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
});
