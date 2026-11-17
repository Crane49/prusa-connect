import { describe, expect, it } from 'vitest';

import { timeTicks, valueScale } from './scale';

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;

describe('valueScale', () => {
  it('covers the values with round steps', () => {
    expect(valueScale(193, 238)).toEqual({
      low: 190,
      high: 240,
      ticks: [190, 200, 210, 220, 230, 240],
    });
  });

  it('steps by 2.5 where that is the round step that fits', () => {
    expect(valueScale(0, 12).ticks).toEqual([0, 2.5, 5, 7.5, 10, 12.5]);
  });

  it('writes a decimal step without rounding noise', () => {
    expect(valueScale(0.1, 0.3).ticks).toEqual([0.1, 0.15, 0.2, 0.25, 0.3]);
  });

  it('covers negative values', () => {
    expect(valueScale(-7, 3)).toEqual({
      low: -8,
      high: 4,
      ticks: [-8, -6, -4, -2, 0, 2, 4],
    });
  });

  it('puts a flat line in the middle of its range', () => {
    const scale = valueScale(215, 215);

    expect(scale.low).toBeLessThan(215);
    expect(scale.high).toBeGreaterThan(215);
    expect(scale.low + scale.high).toBe(430);
  });

  it('gives a flat line at zero a range', () => {
    expect(valueScale(0, 0)).toEqual({
      low: -1,
      high: 1,
      ticks: [-1, -0.5, 0, 0.5, 1],
    });
  });

  describe('with a minimum step', () => {
    it('keeps sensor noise off the edges of a 5 degree axis', () => {
      const scale = valueScale(59.6, 60.3, 5, 5);

      expect(scale.ticks.every((tick) => tick % 5 === 0)).toBe(true);
      expect(scale.low).toBeLessThan(59.6 - 1);
      expect(scale.high).toBeGreaterThan(60.3 + 1);
    });

    it('leaves a wide range with its own round step', () => {
      expect(valueScale(25, 215, 5, 5).ticks).toEqual([
        0, 50, 100, 150, 200, 250,
      ]);
    });

    it('centres a flat line on a multiple of the step', () => {
      expect(valueScale(25, 25, 5, 5).ticks).toEqual([20, 25, 30]);
    });

    it('does not draw a narrow range along the bottom', () => {
      const scale = valueScale(60.1, 60.4, 5, 5);

      expect(60.1 - scale.low).toBeGreaterThanOrEqual(4);
    });
  });

  it('answers a range with no values without throwing', () => {
    expect(valueScale(Infinity, -Infinity).ticks).toEqual([0, 1]);
  });
});

describe('timeTicks', () => {
  it('puts ticks on round minutes of the local clock', () => {
    const from = new Date(2026, 8, 25, 10, 7, 13).getTime();
    const ticks = timeTicks(from, from + 2 * HOUR);

    expect(ticks.map((one) => new Date(one.at).getMinutes())).toEqual([
      30, 0, 30, 0,
    ]);
    expect(ticks.every((one) => !one.day)).toBe(true);
  });

  it('steps over the hour', () => {
    const from = new Date(2026, 8, 25, 10, 50).getTime();
    const ticks = timeTicks(from, from + 20 * MINUTE);

    expect(
      ticks.map((one) => {
        const at = new Date(one.at);

        return `${String(at.getHours())}:${String(at.getMinutes())}`;
      }),
    ).toEqual(['10:50', '10:55', '11:0', '11:5', '11:10']);
  });

  it('names the day at midnight', () => {
    const from = new Date(2026, 8, 25, 22, 10).getTime();
    const ticks = timeTicks(from, from + 4 * HOUR);
    const midnight = new Date(2026, 8, 26).getTime();

    expect(ticks.map((one) => one.at)).toContain(midnight);
    expect(ticks.filter((one) => one.day).map((one) => one.at)).toEqual([
      midnight,
    ]);
  });

  it('steps by days over a long range, each tick at midnight', () => {
    const from = new Date(2026, 8, 20, 15).getTime();
    const ticks = timeTicks(from, new Date(2026, 8, 25, 9).getTime());

    expect(ticks.map((one) => new Date(one.at).getDate())).toEqual([
      21, 22, 23, 24, 25,
    ]);
    expect(
      ticks.every((one) => one.day && new Date(one.at).getHours() === 0),
    ).toBe(true);
  });

  it('keeps an hourly step on the clock face', () => {
    const from = new Date(2026, 8, 25, 1, 20).getTime();
    const ticks = timeTicks(from, from + 14 * HOUR);

    expect(ticks.map((one) => new Date(one.at).getHours())).toEqual([
      3, 6, 9, 12, 15,
    ]);
  });

  // The last Sunday of October 2026 has 25 hours in Prague.
  it('keeps a 3-hour step on the clock face when summer time ends', () => {
    const zone = process.env.TZ;
    process.env.TZ = 'Europe/Prague';
    try {
      const from = new Date(2026, 9, 25, 0, 30).getTime();
      const ticks = timeTicks(from, from + 16 * HOUR);

      expect(ticks.map((one) => new Date(one.at).getHours())).toEqual([
        3, 6, 9, 12, 15,
      ]);
    } finally {
      process.env.TZ = zone;
    }
  });

  it('draws no tick for one instant or a reversed range', () => {
    const at = new Date(2026, 8, 25, 10).getTime();

    expect(timeTicks(at, at)).toEqual([]);
    expect(timeTicks(at + 1, at)).toEqual([]);
    expect(timeTicks(Number.NaN, at)).toEqual([]);
  });

  it('never gives more ticks than asked for', () => {
    const from = new Date(2026, 8, 25, 10, 3).getTime();
    for (const span of [7 * MINUTE, 3 * HOUR, 40 * HOUR, 90 * 24 * HOUR]) {
      expect(timeTicks(from, from + span).length).toBeLessThanOrEqual(6);
    }
  });
});
