import { describe, expect, it } from 'vitest';

import { segmentsOf } from './segments';

const at = (...instants: number[]) =>
  instants.map((instant) => ({ at: instant, value: instant }));

describe('segmentsOf', () => {
  it('splits at a gap', () => {
    const points = [1, 2, Number.NaN, 4, 5].map((value, index) => ({
      at: index,
      value,
    }));

    expect(segmentsOf(points, []).map((run) => run.length)).toEqual([2, 2]);
  });

  it('splits across a break instant', () => {
    expect(segmentsOf(at(0, 10, 20, 30), [15])).toEqual([
      at(0, 10),
      at(20, 30),
    ]);
  });

  it('starts the next segment on a point that a break falls on', () => {
    expect(segmentsOf(at(0, 10, 20, 30), [10])).toEqual([
      at(0),
      at(10, 20, 30),
    ]);
  });

  it('is one segment with no breaks and no gaps', () => {
    expect(segmentsOf(at(0, 10, 20), [])).toEqual([at(0, 10, 20)]);
  });
});
