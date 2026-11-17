import { flushSync } from 'svelte';
import { describe, expect, it } from 'vitest';

import { NearbyPrinters } from './nearby-printers.svelte';
import type { DiscoveryClient, NearbyPrinter } from './services/discovery';
import { fakeClock, settled, StubApiError } from './stubs';

const printerOf = (name: string, address: string): NearbyPrinter => ({
  address,
  name,
  port: 80,
  services: ['_prusalink._tcp'],
  tls: false,
  url: `http://${address}:80/`,
});

const refuse = (): never => {
  throw new Error('this fake answers no visibility');
};

interface Answers {
  rows: readonly NearbyPrinter[];
  refusal: StubApiError | undefined;
  list: number;
}

const stubClient = (
  rows: readonly NearbyPrinter[],
): { client: DiscoveryClient; answers: Answers } => {
  const answers: Answers = { rows, refusal: undefined, list: 0 };
  const client: DiscoveryClient = {
    listNearbyPrinters: () => {
      answers.list += 1;
      if (answers.refusal) return Promise.reject(answers.refusal);

      return Promise.resolve({ items: [...answers.rows] });
    },
    getDiscoveryVisibility: refuse,
    setDiscoveryVisibility: refuse,
  };

  return { client, answers };
};

describe('NearbyPrinters', () => {
  it('starts unread, which is not the same as hearing nobody', () => {
    const store = new NearbyPrinters(stubClient([]).client);

    expect(store.loaded).toBe(false);
    expect(store.loading).toBe(false);
    expect(store.items).toEqual([]);
    expect(store.error).toBeUndefined();
  });

  it('reads the printers the server hears', async () => {
    const rows = [printerOf('Desk MK4', '192.168.1.20')];
    const store = new NearbyPrinters(stubClient(rows).client);

    await store.load();

    expect(store.loaded).toBe(true);
    expect(store.items).toEqual(rows);
  });

  it('tells an empty list from an unread one', async () => {
    const store = new NearbyPrinters(stubClient([]).client);

    await store.load();

    expect(store.loaded).toBe(true);
    expect(store.items).toEqual([]);
  });

  // svelte-data.md: two readers of the same list cost one request.
  it('costs one request however many readers ask at once', async () => {
    const { client, answers } = stubClient([]);
    const store = new NearbyPrinters(client);

    await Promise.all([store.load(), store.load()]);

    expect(answers.list).toBe(1);
  });

  it('reports a refusal by its code and keeps the rows it had', async () => {
    const rows = [printerOf('Desk MK4', '192.168.1.20')];
    const { client, answers } = stubClient(rows);
    const store = new NearbyPrinters(client);
    await store.load();

    answers.refusal = new StubApiError('core.auth/ACCESS_DENIED', 403);
    await store.load();

    expect(store.error?.code).toBe('core.auth/ACCESS_DENIED');
    expect(store.error?.status).toBe(403);
    expect(store.items).toEqual(rows);
  });

  it('reads the list again on the tick, and stops on teardown', async () => {
    const clock = fakeClock();
    const { client, answers } = stubClient([]);
    const store = new NearbyPrinters(client, {
      timers: clock,
      refreshMs: 1000,
    });
    const stop = store.listen();

    clock.advance(1000);
    await settled();
    expect(answers.list).toBe(1);

    clock.advance(1000);
    await settled();
    expect(answers.list).toBe(2);

    stop();
    expect(clock.pending).toBe(0);
  });

  // The list sits on the printers page, which a person may open without the
  // grant to read it. One refusal is enough; the tick does not ask again.
  it('stops the tick once the list is refused with 403', async () => {
    const clock = fakeClock();
    const { client, answers } = stubClient([]);
    answers.refusal = new StubApiError('core.auth/ACCESS_DENIED', 403);
    const store = new NearbyPrinters(client, {
      timers: clock,
      refreshMs: 1000,
    });
    const stop = store.listen();

    clock.advance(1000);
    await settled();
    expect(answers.list).toBe(1);
    expect(clock.pending).toBe(0);

    clock.advance(5000);
    await settled();
    expect(answers.list).toBe(1);
    stop();
  });

  // svelte-testing.md: a loading flip must not replace the rows reference.
  it('draws the list once per answer, not once per loading flip', async () => {
    const rows = [printerOf('Desk MK4', '192.168.1.20')];
    const store = new NearbyPrinters(stubClient(rows).client);
    let draws = 0;
    let seen: readonly unknown[] = [];

    const dispose = $effect.root(() => {
      $effect(() => {
        seen = store.items;
        draws += 1;
      });
    });
    flushSync();
    expect(draws).toBe(1);

    await store.load();
    flushSync();
    expect(draws).toBe(2);
    expect(seen).toEqual(rows);

    dispose();
  });

  it('schedules nothing after a stop, even while a read is in flight', async () => {
    const clock = fakeClock();
    let answer: (() => void) | undefined;
    const client: DiscoveryClient = {
      ...stubClient([]).client,
      listNearbyPrinters: () =>
        new Promise((resolve) => {
          answer = () => {
            resolve({ items: [] });
          };
        }),
    };
    const store = new NearbyPrinters(client, {
      timers: clock,
      refreshMs: 1000,
    });
    const stop = store.listen();

    clock.advance(1000);
    expect(answer).toBeDefined();

    stop();
    answer?.();
    await settled();

    expect(clock.pending).toBe(0);
  });
});

describe('NearbyPrinters reset', () => {
  it('forgets the list, and a read sent before the reset records nothing', async () => {
    const rows = [printerOf('Desk MK4', '192.168.1.20')];
    let answer: (value: { items: NearbyPrinter[] }) => void = () => undefined;
    const client: DiscoveryClient = {
      listNearbyPrinters: () =>
        new Promise((resolve) => {
          answer = resolve;
        }),
      getDiscoveryVisibility: refuse,
      setDiscoveryVisibility: refuse,
    };
    const store = new NearbyPrinters(client);

    const read = store.load();
    store.reset();
    answer({ items: [...rows] });
    await read;
    flushSync();

    expect(store.items).toEqual([]);
    expect(store.loaded).toBe(false);
    expect(store.loading).toBe(false);
  });

  it('reads again after a reset', async () => {
    const rows = [printerOf('Desk MK4', '192.168.1.20')];
    const store = new NearbyPrinters(stubClient(rows).client);

    await store.load();
    store.reset();
    expect(store.items).toEqual([]);

    await store.load();
    expect(store.items).toEqual(rows);
  });
});
