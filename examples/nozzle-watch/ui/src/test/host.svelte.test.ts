import { describe, expect, it } from 'vitest';

import { fakePrinter, stubHost } from './host.svelte';

// The stub's own contract, not a module's: `mark-changed`'s bug (task-11 fix
// round 1) was a key the real `ModuleStore` refuses that this stub used to
// accept, so the unit suite never saw it. This is what keeps that class of
// bug caught here again.
describe('the stub store', () => {
  it('refuses a key that is not one DNS-1123 label, the way ModuleStore does', () => {
    const { host } = stubHost([fakePrinter('printer-1')]);

    host.store.set('changed:printer-1', '2026-09-29T00:00:00.000Z');

    expect(host.store.get('changed:printer-1')).toBeUndefined();
  });

  it('keeps a key that is one DNS-1123 label', () => {
    const { host } = stubHost([fakePrinter('printer-1')]);

    host.store.set('changed', '2026-09-29T00:00:00.000Z');

    expect(host.store.get('changed')).toBe('2026-09-29T00:00:00.000Z');
  });
});
