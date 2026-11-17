import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import setup from '../index';
import FakePicker from '../test/fake-picker.svelte';
import { fakePrinter, type Stub, stubHost } from '../test/host.svelte';

const PRINTER = 'p3f2a1c9b';

const started = (): Stub => {
  const stub = stubHost([fakePrinter(PRINTER)]);
  void setup(stub.host);

  return stub;
};

const drawn = async (stub: Stub): Promise<void> => {
  const tab = stub.registry.sections('device-tab')[0];
  expect(tab, 'the module registered no device-tab tab').toBeDefined();

  await render(tab!.component, { props: { deviceId: PRINTER } });
};

describe('the nozzle-watch tab', () => {
  it('says the mark was never made, before mark-changed ever runs', async () => {
    const stub = started();
    await drawn(stub);

    await expect.element(page.getByText('Last changed')).toBeInTheDocument();
    await expect
      .element(page.getByText('Never marked changed'))
      .toBeInTheDocument();
  });

  it('reads the date mark-changed wrote, off the store the two share', async () => {
    const stub = started();
    const action = stub.registry
      .actions('device-action')
      .find((entry) => entry.id === 'mark-changed');
    expect(
      action,
      'the module registered no mark-changed action',
    ).toBeDefined();
    await action!.run(PRINTER);

    await drawn(stub);

    const year = String(new Date().getFullYear());
    await expect
      .element(page.getByText(year, { exact: false }))
      .toBeInTheDocument();
  });
});

describe('the spool on the nozzle-watch tab', () => {
  it('says what to install when the spools module is not there', async () => {
    const stub = started();
    await drawn(stub);

    await expect
      .element(
        page.getByText(
          'Install the spools extension to pick the spool loaded on this printer.',
        ),
      )
      .toBeInTheDocument();
  });

  it('draws the spools module picker and shows the spool picked in it', async () => {
    const stub = stubHost([fakePrinter(PRINTER)], {
      'com.github.aux.acme.spools/spool-picker': FakePicker,
    });
    void setup(stub.host);
    await drawn(stub);

    await page.getByRole('button', { name: 'petg-black' }).click();

    await expect
      .element(page.getByText('petg-black (PETG) is loaded on this printer.'))
      .toBeInTheDocument();
  });
});
