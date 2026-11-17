import { describe, expect, it, vi } from 'vitest';
import type { Mock } from 'vitest';

import { DeviceSamples, SAMPLES_MAX_PAGES } from './device-samples.svelte';
import type { HistoryClient, HistorySamplePage } from './services/history';

const WINDOW = {
  from: '2026-09-23T08:00:00.000Z',
  to: '2026-09-23T10:00:00.000Z',
  limit: 1000,
};

const sample = (at: string, metric: string, value?: number, text?: string) => ({
  at,
  metric,
  origin: 'device:printer-1',
  ...(value === undefined ? {} : { value }),
  ...(text === undefined ? {} : { text }),
});

const page = (
  items: ReturnType<typeof sample>[],
  cursor = '',
  incomplete = false,
): HistorySamplePage => ({ items, cursor, incomplete });

const clientAnswering = (
  ...pages: HistorySamplePage[]
): {
  client: HistoryClient;
  listDeviceHistorySamples: Mock<HistoryClient['listDeviceHistorySamples']>;
} => {
  const listDeviceHistorySamples: Mock<
    HistoryClient['listDeviceHistorySamples']
  > = vi
    .fn()
    .mockImplementation(() => Promise.resolve(pages.shift() ?? page([])));

  return {
    client: { listDeviceHistoryFacts: vi.fn(), listDeviceHistorySamples },
    listDeviceHistorySamples,
  };
};

describe('DeviceSamples', () => {
  it('groups points by metric, oldest first', async () => {
    const { client } = clientAnswering(
      page([
        sample('2026-09-23T09:00:01.000Z', '/tools/0/nozzle_temp', 215),
        sample('2026-09-23T09:00:00.000Z', '/tools/0/nozzle_temp', 210),
        sample('2026-09-23T09:00:00.000Z', '/temperature/bed', 60),
      ]),
    );
    const store = new DeviceSamples(client, 'printer-1');

    await store.load(WINDOW);

    expect(store.series['/tools/0/nozzle_temp']).toEqual([
      { at: Date.parse('2026-09-23T09:00:00.000Z'), value: 210 },
      { at: Date.parse('2026-09-23T09:00:01.000Z'), value: 215 },
    ]);
    expect(store.series['/temperature/bed']).toHaveLength(1);
  });

  // /state answers a word, and a word is not a point on a line.
  it('drops a reading that carries a word instead of a number', async () => {
    const { client } = clientAnswering(
      page([sample('2026-09-23T09:00:00.000Z', '/state', undefined, 'IDLE')]),
    );
    const store = new DeviceSamples(client, 'printer-1');

    await store.load(WINDOW);

    expect(store.series['/state']).toBeUndefined();
  });

  it('follows the cursor to the end of a slice', async () => {
    const { client } = clientAnswering(
      page([sample('2026-09-23T09:00:00.000Z', '/temperature/bed', 60)], 'a'),
      page([sample('2026-09-23T09:00:01.000Z', '/temperature/bed', 61)]),
    );
    const store = new DeviceSamples(client, 'printer-1');

    await store.load(WINDOW);

    expect(store.series['/temperature/bed']).toHaveLength(2);
    expect(store.truncated).toBe(false);
    expect(store.since).toBe(Date.parse(WINDOW.from));
  });

  // The route answers a window from its oldest row forward, so a walk that
  // began at `from` would spend its budget ninety minutes before the live
  // points it exists to join.
  it('reads the recent end of the window first', async () => {
    const { client, listDeviceHistorySamples } = clientAnswering(
      page([sample('2026-09-23T09:59:00.000Z', '/temperature/bed', 60)]),
    );
    const store = new DeviceSamples(client, 'printer-1');

    await store.load(WINDOW);

    const first = listDeviceHistorySamples.mock.calls[0]?.[0].query;
    expect(first?.to).toBe(WINDOW.to);
    expect(Date.parse(WINDOW.to) - Date.parse(String(first?.from))).toBe(
      300_000,
    );
  });

  // Half a slice would draw a line from the oldest row that was read across
  // a gap to the rows above it, which is a line the printer never reported.
  it('keeps no part of a slice it could not read whole', async () => {
    const dense = Array.from({ length: SAMPLES_MAX_PAGES }, () =>
      page(
        [sample('2026-09-23T09:59:00.000Z', '/temperature/bed', 60)],
        'more',
      ),
    );
    const { client } = clientAnswering(...dense);
    const store = new DeviceSamples(client, 'printer-1');

    await store.load(WINDOW);

    expect(store.series).toEqual({});
    expect(store.truncated).toBe(true);
  });

  // A Raspberry Pi serving a print must not be asked for an unbounded walk.
  it('stops at the page budget and says the window was cut', async () => {
    const pages = Array.from({ length: SAMPLES_MAX_PAGES + 2 }, (_, index) =>
      page(
        [
          sample(
            `2026-09-23T09:00:${String(index).padStart(2, '0')}.000Z`,
            '/temperature/bed',
            60,
          ),
        ],
        'more',
      ),
    );
    const { client, listDeviceHistorySamples } = clientAnswering(...pages);
    const store = new DeviceSamples(client, 'printer-1');

    await store.load(WINDOW);

    expect(listDeviceHistorySamples).toHaveBeenCalledTimes(SAMPLES_MAX_PAGES);
    expect(store.truncated).toBe(true);
  });

  // An effect's cleanup and its rerun happen in one tick, so an abort must
  // drop the flight and not only the controller.
  it('reads again after an abort in the same tick', async () => {
    const { client, listDeviceHistorySamples } = clientAnswering(
      page([sample('2026-09-23T09:00:00.000Z', '/temperature/bed', 60)]),
      page([sample('2026-09-23T09:30:00.000Z', '/temperature/bed', 61)]),
    );
    const store = new DeviceSamples(client, 'printer-1');
    const later = { ...WINDOW, to: '2026-09-23T11:00:00.000Z' };

    const abandoned = store.load(WINDOW);
    store.abort();
    await Promise.all([abandoned, store.load(later)]);

    expect(
      listDeviceHistorySamples.mock.calls.map((call) => call[0].query?.to),
    ).toContain(later.to);
  });

  it('carries incomplete through when any page of the walk was holed, not only the last', async () => {
    const { client } = clientAnswering(
      page(
        [sample('2026-09-23T09:00:00.000Z', '/temperature/bed', 60)],
        'a',
        true,
      ),
      page([sample('2026-09-23T09:00:01.000Z', '/temperature/bed', 61)]),
    );
    const store = new DeviceSamples(client, 'printer-1');

    await store.load(WINDOW);

    expect(store.incomplete).toBe(true);
  });

  it('keeps the refusal as a fault', async () => {
    const client = {
      listDeviceHistoryFacts: vi.fn(),
      listDeviceHistorySamples: vi.fn().mockRejectedValue(
        Object.assign(new Error('no history store is declared'), {
          code: 'core.resource/HISTORY_NOT_CONFIGURED',
        }),
      ),
    };
    const store = new DeviceSamples(client, 'printer-1');

    await store.load(WINDOW);

    expect(store.error?.code).toBe('core.resource/HISTORY_NOT_CONFIGURED');
    expect(store.series).toEqual({});
  });
});
