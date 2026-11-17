import { describe, expect, it } from 'vitest';
import { flushSync } from 'svelte';

import type { JobsReader } from './jobs';
import { daysOf, Stats, type StatsClient } from './stats.svelte';

const NOW = Date.parse('2026-09-28T00:00:00Z');
const fleet = {
  devices: [],
  loading: false,
  loaded: true,
  error: undefined,
  device: () => undefined,
};

/** A client whose answers the test releases by hand, and an event stream it feeds. */
const harness = () => {
  const calls: {
    query: Record<string, unknown>;
    release: (page: unknown) => void;
    fail: (e: unknown) => void;
  }[] = [];
  let push: ((event: { type: string }) => void) | undefined;
  const client = {
    listJobs: (request: { query: Record<string, unknown> }) =>
      new Promise((release, fail) =>
        calls.push({ query: request.query, release, fail }),
      ),
    events: () => ({
      async *[Symbol.asyncIterator]() {
        for (;;)
          yield await new Promise<{ type: string }>(
            (resolve) => (push = resolve),
          );
      },
      close: () => undefined,
    }),
  } as unknown as StatsClient & JobsReader;
  return { client, calls, emit: (type: string) => push?.({ type }) };
};

const settle = () => new Promise((resolve) => setTimeout(resolve, 0));
const empty = { items: [], cursor: '', limit: 200 };

/** One print the jobs read answers, ended on the day before NOW. */
const ended = (deviceId: string) => ({
  id: `${deviceId}-1`,
  device_job_id: '1',
  device_id: deviceId,
  path: 'usb/benchy.bgcode',
  display_name: 'benchy.bgcode',
  state: 'FINISHED',
  progress: 100,
  time_remaining_ms: 0,
  started: '2026-09-27T10:00:00Z',
  ended: '2026-09-27T11:00:00Z',
  outcome: 'finished',
});

describe('Stats', () => {
  it('loads seven days and reads them', async () => {
    const { client, calls } = harness();
    const stats = new Stats(
      client,
      fleet,
      new AbortController().signal,
      () => NOW,
    );
    expect(stats.state.kind).toBe('loading');
    calls[0]?.release(empty);
    await settle();
    flushSync();
    expect(stats.state.kind).toBe('ready');
    expect(calls[0]?.query.to).toBe('2026-09-28T00:00:00.000Z');
  });

  it('keeps the newer range when an older answer arrives late', async () => {
    const { client, calls } = harness();
    const stats = new Stats(
      client,
      fleet,
      new AbortController().signal,
      () => NOW,
    );
    stats.setDays(30);
    calls[1]?.release({ items: [ended('a')], cursor: '', limit: 200 });
    await settle();
    calls[0]?.release(empty);
    await settle();
    flushSync();
    expect(stats.days).toBe(30);
    expect(stats.state).toMatchObject({
      kind: 'ready',
      stats: { kpis: { jobs: 1 } },
    });
  });

  it('reads a refused read as an error, with what was thrown', async () => {
    const { client, calls } = harness();
    const stats = new Stats(
      client,
      fleet,
      new AbortController().signal,
      () => NOW,
    );
    const refusal = Object.assign(new Error('x'), {
      code: 'core.validation/REQUEST_INVALID',
    });
    calls[0]?.fail(refusal);
    await settle();
    flushSync();
    expect(stats.state).toEqual({ kind: 'error', thrown: refusal });
  });

  it('reloads when a job settles and when the stream lost frames', async () => {
    const { client, calls, emit } = harness();
    new Stats(client, fleet, new AbortController().signal, () => NOW);
    calls[0]?.release(empty);
    await settle();
    emit('com.prusa.connect.device.job.settled');
    await settle();
    expect(calls).toHaveLength(2);
    calls[1]?.release(empty);
    await settle();
    emit('com.prusa.connect.stream.continuity.lost');
    await settle();
    expect(calls).toHaveLength(3);
  });

  it('keeps its result when a telemetry frame changes no field it uses', async () => {
    const { client, calls } = harness();
    let rows = $state.raw([
      { id: 'a', name: 'A', model: 'MK4S', tags: [], progress: 1 },
    ]);
    const live = {
      ...fleet,
      get devices() {
        return rows;
      },
    } as unknown as typeof fleet;
    const stats = new Stats(
      client,
      live,
      new AbortController().signal,
      () => NOW,
    );
    calls[0]?.release(empty);
    await settle();
    flushSync();
    const before = stats.state;
    rows = [{ id: 'a', name: 'A', model: 'MK4S', tags: [], progress: 2 }];
    flushSync();
    expect(stats.state).toBe(before);
    rows = [{ id: 'a', name: 'Renamed', model: 'MK4S', tags: [], progress: 2 }];
    flushSync();
    expect(stats.state).not.toBe(before);
  });

  it('reads each period once, however many screens want it', () => {
    const { client, calls } = harness();
    const stats = new Stats(
      client,
      fleet,
      new AbortController().signal,
      () => NOW,
    );
    stats.want(7);
    stats.want(90);
    stats.want(90);
    expect(calls).toHaveLength(2);
    // Ninety days and the ninety before them, which the comparison needs.
    expect(calls[1]?.query.from).toBe('2026-04-01T00:00:00.000Z');
    expect(stats.view(30, { printers: [], tag: '' }).kind).toBe('loading');
  });

  it('reloads every period on screen when a job settles', async () => {
    const { client, calls, emit } = harness();
    const stats = new Stats(
      client,
      fleet,
      new AbortController().signal,
      () => NOW,
    );
    stats.want(30);
    calls[0]?.release(empty);
    calls[1]?.release(empty);
    await settle();
    emit('com.prusa.connect.device.job.settled');
    await settle();
    expect(calls).toHaveLength(4);
  });

  it('works a scope out once, and narrows it to the tagged printers', async () => {
    const { client, calls } = harness();
    const tagged = {
      ...fleet,
      devices: [
        { id: 'a', name: 'A', model: 'MK4S', tags: ['Garage'] },
        { id: 'b', name: 'B', model: 'XL', tags: [] },
      ],
    } as unknown as typeof fleet;
    const stats = new Stats(
      client,
      tagged,
      new AbortController().signal,
      () => NOW,
    );
    calls[0]?.release({
      items: [ended('a'), ended('b')],
      cursor: '',
      limit: 200,
    });
    await settle();
    flushSync();
    const scope = { printers: [], tag: 'garage' };
    const garage = stats.view(7, scope);
    expect(garage).toMatchObject({
      kind: 'ready',
      stats: { kpis: { jobs: 1 } },
    });
    expect(stats.view(7, { printers: [], tag: 'garage' })).toBe(garage);
    expect(stats.view(7, { printers: [], tag: '' })).toMatchObject({
      stats: { kpis: { jobs: 2 } },
    });
  });
});

describe('daysOf', () => {
  it('reads a period a person or a card wrote, else seven days', () => {
    expect(daysOf('30')).toBe(30);
    expect(daysOf(90)).toBe(90);
    expect(daysOf('365')).toBe(7);
    expect(daysOf(undefined)).toBe(7);
  });
});
