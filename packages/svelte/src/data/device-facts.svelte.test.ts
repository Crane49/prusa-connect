import { describe, expect, it, vi } from 'vitest';
import type { Mock } from 'vitest';

import { DeviceFacts } from './device-facts.svelte';
import type { HistoryClient, HistoryFactPage } from './services/history';

const fact = (sequence: number) => ({
  at: '2026-09-23T10:00:00.000Z',
  epoch: 'one',
  origin: 'device:printer-1',
  payload: {},
  sequence,
  type: 'com.prusa.connect.device.state.changed',
});

const page = (
  sequences: readonly number[],
  cursor = '',
  incomplete = false,
): HistoryFactPage => ({
  items: sequences.map(fact),
  cursor,
  incomplete,
});

const WINDOW = {
  from: '2026-09-23T08:00:00.000Z',
  to: '2026-09-23T10:00:00.000Z',
  limit: 200,
};

const WINDOW_2 = {
  from: '2026-09-23T09:00:00.000Z',
  to: '2026-09-23T10:00:00.000Z',
  limit: 200,
};

const clientAnswering = (
  ...pages: HistoryFactPage[]
): {
  client: HistoryClient;
  listDeviceHistoryFacts: Mock<HistoryClient['listDeviceHistoryFacts']>;
} => {
  const listDeviceHistoryFacts: Mock<HistoryClient['listDeviceHistoryFacts']> =
    vi
      .fn()
      .mockImplementation(() => Promise.resolve(pages.shift() ?? page([])));

  return {
    client: {
      listDeviceHistoryFacts,
      listDeviceHistorySamples: vi.fn(),
    },
    listDeviceHistoryFacts,
  };
};

describe('DeviceFacts', () => {
  it('holds the first page and says whether more is waiting', async () => {
    const { client } = clientAnswering(page([1, 2], 'next'));
    const store = new DeviceFacts(client, 'printer-1');

    await store.load(WINDOW);

    expect(store.rows).toHaveLength(2);
    expect(store.more).toBe(true);
    expect(store.loaded).toBe(true);
    expect(store.error).toBeUndefined();
  });

  it('appends the next page and stops when the cursor empties', async () => {
    const { client, listDeviceHistoryFacts } = clientAnswering(
      page([1, 2], 'next'),
      page([3]),
    );
    const store = new DeviceFacts(client, 'printer-1');

    await store.load(WINDOW);
    await store.loadMore(WINDOW);

    expect(store.rows).toHaveLength(3);
    expect(store.more).toBe(false);
    // The resume read carries the cursor the first page answered.
    expect(listDeviceHistoryFacts.mock.calls[1]?.[0].query?.cursor).toBe(
      'next',
    );
  });

  // One read, not two, is what the server is asked for.
  it('gives a second call the read already in flight', async () => {
    const { client, listDeviceHistoryFacts } = clientAnswering(page([1]));
    const store = new DeviceFacts(client, 'printer-1');

    await Promise.all([store.load(WINDOW), store.load(WINDOW)]);

    expect(listDeviceHistoryFacts).toHaveBeenCalledTimes(1);
  });

  it('keeps the refusal as a fault and never as a sentence', async () => {
    const client = {
      listDeviceHistoryFacts: vi.fn().mockRejectedValue(
        Object.assign(new Error('no history store is declared'), {
          code: 'core.resource/HISTORY_NOT_CONFIGURED',
          status: 503,
        }),
      ),
      listDeviceHistorySamples: vi.fn(),
    };
    const store = new DeviceFacts(client, 'printer-1');

    await store.load(WINDOW);

    expect(store.error?.code).toBe('core.resource/HISTORY_NOT_CONFIGURED');
    expect(store.rows).toEqual([]);
    expect(store.loaded).toBe(true);
  });

  // The server reports a hole rather than answering a quiet one.
  it('carries the incomplete flag through', async () => {
    const { client } = clientAnswering(page([1], '', true));
    const store = new DeviceFacts(client, 'printer-1');

    await store.load(WINDOW);

    expect(store.incomplete).toBe(true);
  });

  it('abandons a read for a printer the page has left', async () => {
    const { client, listDeviceHistoryFacts } = clientAnswering(page([1]));
    const store = new DeviceFacts(client, 'printer-1');

    const reading = store.load(WINDOW);
    store.abort();
    await reading;

    expect(listDeviceHistoryFacts.mock.calls[0]?.[1]?.aborted).toBe(true);
  });

  // A Svelte effect's cleanup-then-rerun calls abort() and load() in the
  // same tick. The stale flight `abort()` leaves behind must not be handed
  // back to the window that follows it.
  it('reads the new window when load follows abort in the same tick', async () => {
    const { client, listDeviceHistoryFacts } = clientAnswering(
      page([1]),
      page([2]),
    );
    const store = new DeviceFacts(client, 'printer-1');

    const first = store.load(WINDOW);
    store.abort();
    const second = store.load(WINDOW_2);
    await Promise.all([first, second]);

    expect(listDeviceHistoryFacts).toHaveBeenCalledTimes(2);
    expect(listDeviceHistoryFacts.mock.calls[1]?.[0].query?.from).toBe(
      WINDOW_2.from,
    );
  });
});
