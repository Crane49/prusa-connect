import { describe, expect, it } from 'vitest';

import type { ResourceInstance } from '@prusa/connect-ui/data';

import { json, problem, stubClient } from '../../test/stub';
import { ModuleOperations } from './module-operations.svelte';

const instance = { kind: 'sink', name: 'parts' } as ResourceInstance;
const LOG = 'GET /api/v1/resources/sink/parts/log';
const MEMORY = 'GET /api/v1/resources/sink/parts/memory';

describe('the module operations', () => {
  it('reads the history and the log of an instance', async () => {
    const stub = stubClient({
      [MEMORY]: json(200, {
        items: [{ at: '2026-10-30T10:00:00Z' }],
        stops: [],
      }),
      [LOG]: json(200, { lines: [{ at: '2026-10-30T10:00:00Z', text: 'up' }] }),
    });
    const store = new ModuleOperations(stub.client);

    await store.load(instance);

    // A reading without a value stays absent, never 0.
    expect(store.history(instance)?.items[0]).not.toHaveProperty('value');
    expect(store.log(instance)?.lines[0]?.text).toBe('up');
    expect(store.logRefused(instance)).toBe(false);
  });

  it('says a refused log is refused, not failed', async () => {
    const stub = stubClient({
      [MEMORY]: json(200, { items: [], stops: [] }),
      [LOG]: problem(403, 'core.authorization/FORBIDDEN'),
    });
    const store = new ModuleOperations(stub.client);

    await store.load(instance);

    expect(store.logRefused(instance)).toBe(true);
    expect(store.log(instance)).toBeUndefined();
    expect(store.history(instance)).toBeDefined();
  });

  it('shares one flight between two loads of one instance', async () => {
    const stub = stubClient({
      [MEMORY]: json(200, { items: [], stops: [] }),
      [LOG]: json(200, { lines: [] }),
    });
    const store = new ModuleOperations(stub.client);

    await Promise.all([store.load(instance), store.load(instance)]);

    expect(stub.count(MEMORY)).toBe(1);
    expect(stub.count(LOG)).toBe(1);
  });

  it('keeps a failed read as a fault, and the next good read clears it', async () => {
    let fail = true;
    const stub = stubClient({
      [MEMORY]: () =>
        fail
          ? problem(500, 'core.internal/FAILED')
          : json(200, { items: [], stops: [] }),
      [LOG]: () =>
        fail ? problem(500, 'core.internal/FAILED') : json(200, { lines: [] }),
    });
    const store = new ModuleOperations(stub.client);

    await store.load(instance);
    expect(store.historyFault(instance)).toBeDefined();
    expect(store.logFault(instance)).toBeDefined();
    expect(store.history(instance)).toBeUndefined();

    fail = false;
    await store.load(instance);
    expect(store.historyFault(instance)).toBeUndefined();
    expect(store.logFault(instance)).toBeUndefined();
  });

  it('reads only what it is asked for', async () => {
    const stub = stubClient({
      [MEMORY]: json(200, { items: [], stops: [] }),
      [LOG]: json(200, { lines: [] }),
    });
    const store = new ModuleOperations(stub.client);

    await store.load(instance, 'history');

    expect(stub.count(MEMORY)).toBe(1);
    expect(stub.count(LOG)).toBe(0);
  });

  it('forgets everything on reset', async () => {
    const stub = stubClient({
      [MEMORY]: json(200, { items: [], stops: [] }),
      [LOG]: json(200, { lines: [] }),
    });
    const store = new ModuleOperations(stub.client);
    await store.load(instance);

    store.reset();

    expect(store.history(instance)).toBeUndefined();
    expect(store.log(instance)).toBeUndefined();
  });

  it('keeps the last good reading when a later read fails', async () => {
    let fail = false;
    const stub = stubClient({
      [MEMORY]: () =>
        fail
          ? problem(500, 'core.api/INTERNAL')
          : json(200, { items: [], stops: [] }),
      [LOG]: () =>
        fail
          ? problem(500, 'core.api/INTERNAL')
          : json(200, { lines: [{ at: '2026-10-30T10:00:00Z', text: 'up' }] }),
    });
    const store = new ModuleOperations(stub.client);
    await store.load(instance);

    fail = true;
    await store.load(instance);

    expect(store.history(instance)).toBeDefined();
    expect(store.log(instance)?.lines).toHaveLength(1);
    expect(store.missing(instance)).toBe(false);
  });

  it('marks an instance the server does not know as missing, and clears a stale refusal', async () => {
    let status = 403;
    const stub = stubClient({
      [MEMORY]: json(200, { items: [], stops: [] }),
      [LOG]: () => problem(status, 'core.api/X'),
    });
    const store = new ModuleOperations(stub.client);
    await store.load(instance);
    expect(store.logRefused(instance)).toBe(true);

    status = 404;
    await store.load(instance);

    expect(store.logRefused(instance)).toBe(false);
    expect(store.missing(instance)).toBe(true);
  });

  it('keeps one flight when a reset replaced the one in flight', async () => {
    const stub = stubClient({
      [MEMORY]: json(200, { items: [], stops: [] }),
      [LOG]: json(200, { lines: [] }),
    });
    const store = new ModuleOperations(stub.client);

    const first = store.load(instance);
    store.reset();
    const second = store.load(instance);
    await first;
    // The stale flight finishing must not forget the newer one.
    await store.load(instance);
    await second;

    expect(stub.count(MEMORY)).toBe(2);
  });
});
