import { describe, expect, it } from 'vitest';

import setup from './index';
import { changedAt } from './mark-changed';
import { fakePrinter, stubHost } from './test/host.svelte';

// Driven through the registry, the way the app's own kebab menu drives it
// (packages/svelte/src/slots/slot-actions/slot-actions.svelte): find the run
// function the module registered and call it with a printer's id.

describe('the nozzle-watch mark-changed action', () => {
  it("writes today's date under the printer it was run for", async () => {
    const stub = stubHost([fakePrinter('printer-1')]);
    void setup(stub.host);
    const action = stub.registry
      .actions('device-action')
      .find((entry) => entry.id === 'mark-changed');
    expect(
      action,
      'the module registered no mark-changed action',
    ).toBeDefined();

    await action!.run('printer-1');

    const today = new Date().toISOString().slice(0, 10);
    expect(changedAt(stub.host, 'printer-1')).toContain(today);
  });

  it('is refused for a printer this page never loaded', async () => {
    const stub = stubHost([fakePrinter('printer-1')]);
    void setup(stub.host);
    const action = stub.registry
      .actions('device-action')
      .find((entry) => entry.id === 'mark-changed');
    expect(
      action,
      'the module registered no mark-changed action',
    ).toBeDefined();

    await expect(action!.run('missing')).rejects.toThrow(
      'This printer is not in the fleet this page read.',
    );
    expect(changedAt(stub.host, 'missing')).toBeUndefined();
  });
});
