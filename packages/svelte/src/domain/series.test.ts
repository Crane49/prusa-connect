import { describe, expect, it } from 'vitest';

import { mergeSeries, type TelemetryPoint, thinSeries } from './series';

const at = (...values: readonly [number, number][]): TelemetryPoint[] =>
  values.map(([when, value]) => ({ at: when, value }));

describe('mergeSeries', () => {
  it('puts one series after the other, oldest first', () => {
    const merged = mergeSeries(at([1, 10], [2, 20]), at([3, 30]));

    expect(merged).toEqual(at([1, 10], [2, 20], [3, 30]));
  });

  it('orders points the caller handed over out of order', () => {
    const merged = mergeSeries(at([3, 30], [1, 10]), at([2, 20]));

    expect(merged).toEqual(at([1, 10], [2, 20], [3, 30]));
  });

  // The stored point carries the instant the origin measured, which is the
  // instant the rest of the series is spelled in.
  it('keeps the stored point where both sides carry one instant', () => {
    const merged = mergeSeries(at([2, 20]), at([2, 99]));

    expect(merged).toEqual(at([2, 20]));
  });

  it('answers the other side where one is empty', () => {
    expect(mergeSeries([], at([1, 10]))).toEqual(at([1, 10]));
    expect(mergeSeries(at([1, 10]), [])).toEqual(at([1, 10]));
    expect(mergeSeries([], [])).toEqual([]);
  });
});

describe('thinSeries', () => {
  it('leaves a series already inside the budget alone', () => {
    const points = at([1, 10], [2, 20], [3, 30]);

    expect(thinSeries(points, 600)).toBe(points);
  });

  it('holds the budget', () => {
    const points = Array.from({ length: 5000 }, (_, index) => ({
      at: index,
      value: index % 100,
    }));

    expect(thinSeries(points, 600).length).toBeLessThanOrEqual(600);
  });

  // A mean would hide the spike that is the reason somebody opened the tab.
  it('keeps the highest and the lowest reading of the window', () => {
    const points = Array.from({ length: 5000 }, (_, index) => ({
      at: index,
      value: index === 2500 ? 300 : 20,
    }));

    const thinned = thinSeries(points, 600);

    expect(thinned.some((point) => point.value === 300)).toBe(true);
  });

  it('keeps the first and the last point', () => {
    const points = Array.from({ length: 5000 }, (_, index) => ({
      at: index,
      value: index,
    }));

    const thinned = thinSeries(points, 600);

    expect(thinned[0]).toEqual({ at: 0, value: 0 });
    expect(thinned.at(-1)).toEqual({ at: 4999, value: 4999 });
  });

  it('answers an empty series unchanged', () => {
    expect(thinSeries([], 600)).toEqual([]);
  });
});
