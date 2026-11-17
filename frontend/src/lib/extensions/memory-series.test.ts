import { describe, expect, it } from 'vitest';

import type { ResourceMemoryHistory } from '../stores/module-operations.svelte';
import { memorySeries } from './memory-series';
import type { ResourceMemory } from './stops';

const t = (key: string) => key;
const MIB = 1024 * 1024;
const NOW = Date.parse('2026-10-30T11:00:00Z');
const at = (seconds: number) =>
  new Date(Date.parse('2026-10-30T10:00:00Z') + seconds * 1000).toISOString();

const memory = (over: Partial<ResourceMemory> = {}): ResourceMemory =>
  ({
    budget_bytes: 128 * MIB,
    held_until_restart: false,
    switch_off_window_seconds: 3600,
    ...over,
  }) as ResourceMemory;

const history = (
  items: ResourceMemoryHistory['items'],
  stops: ResourceMemoryHistory['stops'] = [],
): ResourceMemoryHistory => ({ items, stops });

describe('memorySeries', () => {
  it('an unread sample is a gap, never zero', () => {
    const { points } = memorySeries(
      history([{ at: at(0), value: 64 * MIB }, { at: at(2) }]),
      memory(),
      NOW,
      t,
    );

    expect(points[0]?.value).toBe(64);
    expect(points[1]?.value).toBeNaN();
  });

  it('a stop is a marker and a span to the next reading', () => {
    const series = memorySeries(
      history(
        [
          { at: at(0), value: MIB },
          { at: at(620), value: MIB },
        ],
        [{ at: at(600), result: 'over-memory' }],
      ),
      memory(),
      NOW,
      t,
    );

    expect(series.markers.map((one) => one.at)).toEqual([Date.parse(at(600))]);
    expect(series.regions).toMatchObject([
      { from: Date.parse(at(600)), to: Date.parse(at(620)) },
    ]);
  });

  it('a span with no reading after it runs to now', () => {
    const { regions } = memorySeries(
      history([{ at: at(0), value: MIB }], [{ at: at(600), result: 'exited' }]),
      memory(),
      NOW,
      t,
    );

    expect(regions[0]?.to).toBe(NOW);
  });

  it('a module kept off runs its last span to now', () => {
    const { regions } = memorySeries(
      history(
        [
          { at: at(0), value: MIB },
          { at: at(700), value: MIB },
        ],
        [{ at: at(600), result: 'exited' }],
      ),
      memory({ held_until_restart: true }),
      NOW,
      t,
    );

    expect(regions[0]?.to).toBe(NOW);
  });

  it('the budget is the threshold in MiB, over the last hour', () => {
    const series = memorySeries(history([]), memory(), NOW, t);

    expect(series.threshold.value).toBe(128);
    expect(series.domain).toEqual([NOW - 3_600_000, NOW]);
  });

  it('while switched off only the last span runs to now', () => {
    const { regions } = memorySeries(
      history(
        [
          { at: at(0), value: MIB },
          { at: at(320), value: MIB },
        ],
        [
          { at: at(300), result: 'exited' },
          { at: at(400), result: 'exited' },
        ],
      ),
      memory({ switched_off: { at: at(400), stops: [] } }),
      NOW,
      t,
    );

    expect(regions[0]?.to).toBe(Date.parse(at(320)));
    expect(regions[1]?.to).toBe(NOW);
  });

  it('a reading past the browser clock extends the hour to it', () => {
    const ahead = NOW + 60_000;
    const series = memorySeries(
      history([{ at: new Date(ahead).toISOString(), value: MIB }]),
      memory(),
      NOW,
      t,
    );

    expect(series.domain[1]).toBe(ahead);
  });
});
