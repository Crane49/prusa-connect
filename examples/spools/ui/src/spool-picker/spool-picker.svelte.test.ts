import type { Component } from 'svelte';
import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import setup from '../index';
import type { Spool } from '../spools';
import { stubHost } from '../test/host';
import type { SpoolPickerProps } from './types';

// The picker as another module's page draws it: the component this module's
// setup registered under ui.exports, with the props and the callback the
// other page passes (ADR-20261027-wvj rules 19 to 23).

const SHELF = {
  spools: [
    { id: 'petg-black', material: 'PETG', colour: 'black', grams: 740 },
    { id: 'pla-galaxy', material: 'PLA', colour: 'galaxy black', grams: 210 },
  ],
  principal: 'user:demo',
};

describe('the spool picker', () => {
  it('is the one component the manifest exports', () => {
    const stub = stubHost([{ status: 200, body: SHELF }]);
    void setup(stub.host);

    expect(Object.keys(stub.registered.components)).toEqual(['spool-picker']);
  });

  it('hands the spool a person picked back through onselect', async () => {
    const stub = stubHost([{ status: 200, body: SHELF }]);
    void setup(stub.host);

    const picked: Spool[] = [];
    const props: SpoolPickerProps = {
      onselect: (spool) => picked.push(spool),
      selected: 'petg-black',
    };
    // The registry holds a component under its narrowest type; the props are
    // the ones this module publishes.
    const Picker = stub.registered.components['spool-picker'] as unknown as
      Component<SpoolPickerProps> | undefined;
    expect(Picker, 'the module registered no picker').toBeDefined();

    await render(Picker!, { props });

    await expect
      .element(page.getByRole('button', { name: 'petg-black' }))
      .toHaveAttribute('aria-pressed', 'true');
    await page.getByRole('button', { name: 'pla-galaxy' }).click();

    expect(picked.map((spool) => spool.id)).toEqual(['pla-galaxy']);
  });
});
