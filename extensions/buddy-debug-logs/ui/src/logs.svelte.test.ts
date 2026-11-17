import { flushSync } from 'svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { LogLine, LogsApi } from './api';
import { KEEP, LIVE_EVERY, LogsStore, PAGE } from './logs.svelte';

type Query = NonNullable<Parameters<LogsApi['listLogs']>[0]>['query'];
type Page = Awaited<ReturnType<LogsApi['listLogs']>>;

const line = (cursor: string, text = cursor): LogLine => ({
  cursor,
  at: '2026-10-19T14:00:00Z',
  severity: 'info',
  text,
});

/** A listLogs that records each query and answers from a function of it. */
const fake = (answer: (query: Query) => Page) => {
  const asked: Query[] = [];

  return {
    asked,
    api: {
      listLogs: (request?: { query?: Query }) => {
        asked.push(request?.query);

        return Promise.resolve(answer(request?.query));
      },
    } as Pick<LogsApi, 'listLogs'>,
  };
};

const NOW = Date.parse('2026-10-19T15:00:00Z');

afterEach(() => {
  vi.useRealTimers();
});

describe('LogsStore', () => {
  it('asks for the newest page of the window and the filters', async () => {
    const { api, asked } = fake(() => ({
      lines: [line('b'), line('a')],
      older: 'a',
      newest: 'b',
    }));
    const logs = new LogsStore(
      api,
      { origin: { kind: 'device', id: 'mk4' } },
      () => NOW,
    );

    await logs.set({ level: 'warning', component: ' Connect ', text: 'lost' });
    flushSync();

    expect(asked.at(-1)).toEqual({
      device: 'mk4',
      since: '2026-10-18T15:00:00.000Z',
      severity: 'warning',
      component: 'Connect',
      q: 'lost',
      limit: PAGE,
    });
    expect(logs.lines?.map((one) => one.cursor)).toEqual(['b', 'a']);
    expect(logs.hasOlder).toBe(true);
  });

  it('adds the older page under what is shown, with the cursor the module gave', async () => {
    const { api, asked } = fake((query) =>
      query?.before === 'a'
        ? { lines: [line('z')] }
        : { lines: [line('b'), line('a')], older: 'a', newest: 'b' },
    );
    const logs = new LogsStore(api, {}, () => NOW);

    await logs.load();
    await logs.loadOlder();
    flushSync();

    expect(asked.at(-1)?.before).toBe('a');
    expect(logs.lines?.map((one) => one.cursor)).toEqual(['b', 'a', 'z']);
    expect(logs.hasOlder).toBe(false);
  });

  it('puts newer lines on top when live, from the newest cursor and not the window', async () => {
    const { api, asked } = fake((query) =>
      query?.after === 'b'
        ? { lines: [line('d'), line('c')], newest: 'd' }
        : { lines: [line('b')], newest: 'b' },
    );
    const logs = new LogsStore(api, {}, () => NOW);

    await logs.load();
    await logs.poll();
    flushSync();

    expect(asked.at(-1)).toEqual({ after: 'b', limit: PAGE });
    expect(logs.lines?.map((one) => one.cursor)).toEqual(['d', 'c', 'b']);
  });

  it('keeps at most the newest lines a view holds', async () => {
    let next = 0;
    const { api } = fake((query) => {
      if (query?.after === undefined)
        return { lines: [line('start')], newest: 'start' };
      next += 1;

      return {
        lines: Array.from({ length: PAGE }, (_, i) =>
          line(`n${String(next)}-${String(i)}`),
        ),
        newest: query.after,
      };
    });
    const logs = new LogsStore(api, {}, () => NOW);

    await logs.load();
    for (let round = 0; round < 15; round += 1) await logs.poll();

    expect(logs.lines?.length).toBe(KEEP);
  });

  it('asks every two seconds while live, and stops when the view goes', async () => {
    vi.useFakeTimers();
    const { api, asked } = fake(() => ({ lines: [], newest: 'b' }));
    const logs = new LogsStore(api, {}, () => NOW);
    await logs.load();
    const before = asked.length;

    logs.setLive(true);
    await vi.advanceTimersByTimeAsync(LIVE_EVERY * 3);
    const during = asked.length - before;
    logs.dispose();
    await vi.advanceTimersByTimeAsync(LIVE_EVERY * 3);

    expect(during).toBe(3);
    expect(asked.length - before).toBe(3);
    expect(logs.live).toBe(false);
  });

  it('keeps the newer filter when an older answer arrives late', async () => {
    const slow: { resolve?: (page: Page) => void } = {};
    let calls = 0;
    const api = {
      listLogs: (request?: { query?: Query }) => {
        calls += 1;
        if (calls === 1) {
          return new Promise<Page>((resolve) => {
            slow.resolve = resolve;
          });
        }

        return Promise.resolve({ lines: [line(request?.query?.q ?? 'none')] });
      },
    } as Pick<LogsApi, 'listLogs'>;
    const logs = new LogsStore(api, {}, () => NOW);

    const first = logs.load();
    await logs.set({ text: 'fresh' });
    slow.resolve?.({ lines: [line('stale')] });
    await first;
    flushSync();

    expect(logs.lines?.map((one) => one.cursor)).toEqual(['fresh']);
  });

  it('tells nothing matched from nothing arrived, and keeps a refusal', async () => {
    const empty = new LogsStore(fake(() => ({ lines: [] })).api, {}, () => NOW);
    await empty.load();
    expect(empty.lines).toEqual([]);

    const refusing = new LogsStore(
      { listLogs: () => Promise.reject(new Error('down')) },
      {},
      () => NOW,
    );
    await refusing.load();
    expect(refusing.lines).toBeUndefined();
    expect(refusing.fault).toBeInstanceOf(Error);
  });

  it('reads the newest page again when the module refuses a stale cursor', async () => {
    const asked: Query[] = [];
    let moved = false;
    const api = {
      listLogs: (request?: { query?: Query }) => {
        asked.push(request?.query);
        if (request?.query?.after !== undefined && !moved) {
          moved = true;

          return Promise.reject(
            Object.assign(new Error('moved'), {
              code: 'buddy-debug-logs/CURSOR_STALE',
            }),
          );
        }

        return Promise.resolve(
          moved
            ? { lines: [line('d'), line('c')], newest: 'd' }
            : { lines: [line('b'), line('a')], newest: 'b' },
        );
      },
    } as Pick<LogsApi, 'listLogs'>;
    const logs = new LogsStore(api, {}, () => NOW);

    await logs.load();
    await logs.poll();
    flushSync();

    expect(asked.map((query) => query?.after)).toEqual([
      undefined,
      'b',
      undefined,
    ]);
    expect(logs.lines?.map((one) => one.cursor)).toEqual(['d', 'c']);
    expect(logs.fault).toBeUndefined();
  });
});
