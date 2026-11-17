import { describe, expect, it, vi } from 'vitest';

import { DeviceHistory } from './device-history.svelte';
import type { HistoryClient } from './services/history';

const NOW = Date.parse('2026-09-23T10:00:00.000Z');
const HOUR = 3_600_000;

const client = (): HistoryClient & {
  facts: ReturnType<typeof vi.fn>;
  samples: ReturnType<typeof vi.fn>;
} => {
  const facts = vi
    .fn()
    .mockResolvedValue({ items: [], cursor: '', incomplete: false });
  const samples = vi
    .fn()
    .mockResolvedValue({ items: [], cursor: '', incomplete: false });

  return {
    facts,
    samples,
    listDeviceHistoryFacts: facts,
    listDeviceHistorySamples: samples,
  };
};

interface Query {
  query: Record<string, unknown>;
}

const queryOf = (
  mock: ReturnType<typeof vi.fn>,
  call = 0,
): Record<string, unknown> => (mock.mock.calls[call]?.[0] as Query).query;

describe('DeviceHistory', () => {
  it('reads nothing until the tab opens', () => {
    const held = client();
    new DeviceHistory(held, 'printer-1', { hours: 2, now: () => NOW });

    expect(held.facts).not.toHaveBeenCalled();
    expect(held.samples).not.toHaveBeenCalled();
  });

  // The chart draws the metrics a reader chose, so the read asks for those
  // and not for every path the printer reports.
  it('asks only for the metrics the chart draws', async () => {
    const held = client();
    const history = new DeviceHistory(held, 'printer-1', {
      hours: 2,
      now: () => NOW,
    });

    await history.open(['/tools/0/nozzle_temp', '/temperature/bed']);

    expect(queryOf(held.samples).metrics).toEqual([
      '/tools/0/nozzle_temp',
      '/temperature/bed',
    ]);
    expect(history.span).toEqual({ from: NOW - 2 * HOUR, to: NOW });
  });

  it('reads the events with the classes it was built with', async () => {
    const held = client();
    const history = new DeviceHistory(held, 'printer-1', {
      hours: 2,
      classes: ['state', 'command'],
      now: () => NOW,
    });

    await history.open([]);

    expect(queryOf(held.facts).classes).toEqual(['state', 'command']);
  });

  it('reads the measurements again for a metric it has not asked for', async () => {
    const held = client();
    const history = new DeviceHistory(held, 'printer-1', {
      hours: 2,
      now: () => NOW,
    });
    await history.open(['/tools/0/nozzle_temp']);
    const opened = held.samples.mock.calls.length;

    await history.draw(['/tools/0/nozzle_temp']);
    expect(held.samples).toHaveBeenCalledTimes(opened);

    await history.draw(['/tools/0/nozzle_temp', '/axis/x']);
    expect(held.samples.mock.calls.length).toBeGreaterThan(opened);
    expect(queryOf(held.samples, opened).metrics).toEqual([
      '/tools/0/nozzle_temp',
      '/axis/x',
    ]);
    // The events are not read again for a change to the chart.
    expect(held.facts).toHaveBeenCalledTimes(1);
  });

  it('keeps the metrics it asked for before when it reads again', async () => {
    const held = client();
    const history = new DeviceHistory(held, 'printer-1', {
      hours: 2,
      now: () => NOW,
    });
    await history.open(['/temperature/bed']);
    const opened = held.samples.mock.calls.length;

    await history.draw(['/temperature/bed_target']);

    expect(queryOf(held.samples, opened).metrics).toEqual([
      '/temperature/bed',
      '/temperature/bed_target',
    ]);
  });

  it('does not read again after a read of every metric', async () => {
    const held = client();
    const history = new DeviceHistory(held, 'printer-1', {
      hours: 2,
      now: () => NOW,
    });
    await history.open([]);
    const opened = held.samples.mock.calls.length;

    await history.draw(['/axis/x']);

    expect(held.samples).toHaveBeenCalledTimes(opened);
  });

  it('pages the events within the window it opened on', async () => {
    const held = client();
    held.facts.mockResolvedValueOnce({
      items: [],
      cursor: 'next',
      incomplete: false,
    });
    const history = new DeviceHistory(held, 'printer-1', {
      hours: 2,
      now: () => NOW,
    });
    await history.open([]);

    await history.more();

    expect(queryOf(held.facts, 1).cursor).toBe('next');
    expect(queryOf(held.facts, 1).to).toBe(queryOf(held.facts, 0).to);
  });
});
