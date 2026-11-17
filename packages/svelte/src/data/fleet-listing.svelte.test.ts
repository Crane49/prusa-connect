import type { EventType, StreamEvent } from '@prusa/connect';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Device } from '../domain/command';
import { STATE_PATH, textAt } from '../domain/state-paths';
import { fakeDevice } from '../test/fakes';
import { FleetListing, type FleetListingClient } from './fleet-listing.svelte';

type Page = Awaited<
  ReturnType<ConstructorParameters<typeof FleetListing>[0]['listDevices']>
>;

const pageOf = (items: Device[], over: Partial<Page> = {}): Page => ({
  filters: [],
  issues: { affected: 0, by_severity: {}, counts: {} },
  items,
  limit: 50,
  offset: 0,
  order: 'asc',
  sort: 'urgency',
  sorts: [{ default_order: 'asc', field: 'name' }],
  total: items.length,
  ...over,
});

const row = (id: string, state?: string): Device => ({
  ...fakeDevice(id),
  state: state === undefined ? {} : { [STATE_PATH]: state },
});

const deferred = <T>() => {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });

  return { promise, resolve };
};

const eventOf = (
  type: Exclude<
    EventType,
    | 'com.prusa.connect.stream.continuity.lost'
    | 'com.prusa.connect.stream.origin.withdrawn'
  >,
  deviceId: string,
  payload: unknown = {},
): StreamEvent => ({
  type,
  id: '1',
  payload,
  envelope: {
    specversion: '1.0',
    id: '1',
    source: `urn:prusa:connect:printer/${deviceId}`,
    type,
    time: '2026-09-11T00:00:00Z',
    subject: `device:${deviceId}`,
    hops: 0,
    evclass: 'state',
  },
});

const presenceChanged = (id: string) =>
  eventOf('com.prusa.connect.device.presence.changed', id);

const telemetry = (id: string, state: string) =>
  eventOf('com.prusa.connect.device.telemetry.sampled', id, {
    path: STATE_PATH,
    value: state,
  });

const stateOf = (device: Device | undefined) =>
  device === undefined ? undefined : textAt(device, STATE_PATH);

describe('FleetListing', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it('asks the server for the page, filters, search and order it holds', async () => {
    const listDevices = vi.fn(() => Promise.resolve(pageOf([])));
    const listing = new FleetListing({ listDevices });

    listing.setQuery({
      page: 2,
      pageSize: 25,
      filters: { state: ['IDLE'], model: [] },
      search: ' brno ',
      sort: 'name',
      order: 'desc',
    });
    await vi.runAllTimersAsync();

    expect(listDevices).toHaveBeenLastCalledWith(
      {
        query: {
          limit: 25,
          offset: 50,
          state: ['IDLE'],
          search: 'brno',
          sort: 'name',
          order: 'desc',
        },
      },
      expect.any(AbortSignal),
    );
  });

  it('sends the retired choice only when it is not the default', async () => {
    const listDevices = vi.fn<FleetListingClient['listDevices']>(() =>
      Promise.resolve(pageOf([])),
    );
    const listing = new FleetListing({ listDevices });

    listing.setQuery({ retired: 'exclude' });
    await vi.runAllTimersAsync();
    expect(listDevices.mock.lastCall?.[0]?.query).not.toHaveProperty('retired');

    listing.setQuery({ retired: 'only' });
    await vi.runAllTimersAsync();
    expect(listDevices.mock.lastCall?.[0]?.query).toHaveProperty(
      'retired',
      'only',
    );
  });

  it('sends no sort the server did not advertise', async () => {
    const listDevices = vi.fn<FleetListingClient['listDevices']>(() =>
      Promise.resolve(pageOf([])),
    );
    const listing = new FleetListing({ listDevices });
    listing.setQuery({});
    await vi.runAllTimersAsync();

    listing.setQuery({ sort: 'serial' });
    await vi.runAllTimersAsync();

    expect(listDevices.mock.lastCall?.[0]?.query).not.toHaveProperty('sort');
  });

  it('sends no sort outside the SDK enum on the first read', async () => {
    const listDevices = vi.fn<FleetListingClient['listDevices']>(() =>
      Promise.resolve(pageOf([])),
    );
    const listing = new FleetListing({ listDevices });

    listing.setQuery({ sort: 'temperature' });
    await vi.runAllTimersAsync();

    expect(listDevices.mock.lastCall?.[0]?.query).not.toHaveProperty('sort');
  });

  it('reads once and stops when an empty page lies inside the total', async () => {
    const listDevices = vi.fn(() =>
      Promise.resolve(pageOf([], { total: 30, offset: 0, limit: 25 })),
    );
    const listing = new FleetListing({ listDevices });

    listing.setQuery({ pageSize: 25 });
    await vi.runAllTimersAsync();

    expect(listDevices).toHaveBeenCalledTimes(1);
  });

  it('does not loop when setQuery is called from an effect', async () => {
    const listDevices = vi.fn(() => Promise.resolve(pageOf([])));
    const listing = new FleetListing({ listDevices });

    const stop = $effect.root(() => {
      $effect(() => {
        listing.setQuery({ search: 'a' });
      });
    });
    await vi.runAllTimersAsync();
    stop();

    expect(listDevices).toHaveBeenCalledTimes(1);
  });

  it('drops an answer to a query it no longer holds', async () => {
    const slow = deferred<Page>();
    const listDevices = vi
      .fn()
      .mockReturnValueOnce(slow.promise)
      .mockResolvedValueOnce(pageOf([row('new')]));
    const listing = new FleetListing({ listDevices });

    listing.setQuery({ search: 'a' });
    listing.setQuery({ search: 'ab' });
    await vi.runAllTimersAsync();
    slow.resolve(pageOf([row('old')]));
    await vi.runAllTimersAsync();

    expect(listing.items.map((item) => item.id)).toEqual(['new']);
  });

  it('reads the page once for a burst of events', async () => {
    const listDevices = vi.fn(() => Promise.resolve(pageOf([])));
    const listing = new FleetListing({ listDevices }, { debounceMs: 300 });
    listing.setQuery({});
    await vi.runAllTimersAsync();
    listDevices.mockClear();

    for (let index = 0; index < 20; index += 1) {
      listing.hear(presenceChanged(`d${String(index)}`));
    }
    await vi.advanceTimersByTimeAsync(300);

    expect(listDevices).toHaveBeenCalledTimes(1);
  });

  it('reads the page no more for a transfer or a folder read', async () => {
    const listDevices = vi.fn(() => Promise.resolve(pageOf([])));
    const listing = new FleetListing({ listDevices }, { debounceMs: 300 });
    listing.setQuery({});
    await vi.runAllTimersAsync();
    listDevices.mockClear();

    listing.hear(eventOf('com.prusa.connect.device.transfer.progressed', 'd1'));
    listing.hear(eventOf('com.prusa.connect.device.files.changed', 'd1'));
    await vi.advanceTimersByTimeAsync(300);

    expect(listDevices).not.toHaveBeenCalled();
  });

  it('patches a row on the page from a telemetry frame without asking again', async () => {
    const listDevices = vi.fn(() =>
      Promise.resolve(pageOf([row('d1', 'IDLE')])),
    );
    const listing = new FleetListing({ listDevices });
    listing.setQuery({});
    await vi.runAllTimersAsync();
    listDevices.mockClear();

    listing.hear(telemetry('d1', 'PRINTING'));

    expect(listDevices).not.toHaveBeenCalled();
    expect(stateOf(listing.items[0])).toBe('PRINTING');
  });

  it('steps back to the last page when the fleet shrank under it', async () => {
    const listDevices = vi
      .fn()
      .mockResolvedValueOnce(pageOf([], { total: 30, offset: 50, limit: 25 }))
      .mockResolvedValueOnce(
        pageOf([row('d29')], { total: 30, offset: 25, limit: 25 }),
      );
    const listing = new FleetListing({ listDevices });

    listing.setQuery({ page: 2, pageSize: 25 });
    await vi.runAllTimersAsync();

    expect(listing.query.page).toBe(1);
    expect(listing.items.map((item) => item.id)).toEqual(['d29']);
  });

  it('keeps a failed read in error and never throws from hear', async () => {
    const listDevices = vi.fn().mockRejectedValue(new Error('down'));
    const listing = new FleetListing({ listDevices });
    listing.setQuery({});
    await vi.runAllTimersAsync();

    expect(listing.error?.message).toBe('down');
    expect(() => {
      listing.hear({} as StreamEvent);
    }).not.toThrow();
  });

  it('cancels a pending refetch on close', async () => {
    const listDevices = vi.fn(() => Promise.resolve(pageOf([])));
    const listing = new FleetListing({ listDevices }, { debounceMs: 300 });
    listing.setQuery({});
    await vi.runAllTimersAsync();
    listDevices.mockClear();

    listing.hear(presenceChanged('d1'));
    listing.close();
    await vi.advanceTimersByTimeAsync(300);

    expect(listDevices).not.toHaveBeenCalled();
  });
});
