import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import setup from '../index';
import { fakePrinter, type Stub, stubHost } from '../test/host.svelte';

// Driven the way nozzle-section.svelte.test.ts drives the section: the stub
// host runs the bundle's own setup, and each case renders the device-card
// component the setup registered.

const PRINTER = 'p3f2a1c9b';

const started = (state: Record<string, unknown> = {}): Stub => {
  const stub = stubHost([fakePrinter(PRINTER, state)]);
  void setup(stub.host);

  return stub;
};

const drawn = async (stub: Stub, deviceId = PRINTER): Promise<void> => {
  const line = stub.registry.sections('device-card')[0];
  expect(line, 'the module registered no device-card line').toBeDefined();

  await render(line!.component, { props: { deviceId } });
};

describe('the nozzle-watch card line', () => {
  it('shows the diameter and the material a driver reports', async () => {
    const stub = started({
      '/tools/0/features/nozzle_diameter': 0.4,
      '/tools/0/material_package_instance/package/material/abbreviation':
        'PETG',
    });
    await drawn(stub);

    await expect.element(page.getByText('0.4 mm PETG')).toBeInTheDocument();
  });

  it('falls back to the reading nozzle.ts already reads, past no diameter or material', async () => {
    const stub = started({
      '/tools/0/nozzle_temp': 214.8,
      '/tools/0/nozzle_target_temp': 215,
    });
    await drawn(stub);

    await expect
      .element(page.getByText('214.8°C, target 215°C'))
      .toBeInTheDocument();
  });

  it('says nothing it cannot know about a printer that is not in the fleet', async () => {
    const stub = started();
    await drawn(stub, 'a-printer-this-person-may-not-see');

    await expect
      .element(page.getByText(/not in the fleet this page read/))
      .toBeInTheDocument();
  });
});
