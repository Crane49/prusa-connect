// The tick arithmetic of the line chart, kept apart from the markup so a test
// can check it without a browser.

/** A value axis: the range it spans and the values it labels, lowest first. */
export interface ValueScale {
  readonly low: number;
  readonly high: number;
  readonly ticks: readonly number[];
}

/** The step multipliers a person reads as round numbers. */
const NICE_STEPS = [1, 2, 2.5, 5, 10];

// Adding a step many times collects binary rounding errors, so a tick such as
// 0.30000000000000004 is rounded back to the number a person expects.
const tidy = (value: number): number =>
  Number.parseFloat(value.toPrecision(12));

const niceStep = (span: number, count: number): number => {
  const rough = span / count;
  const power = 10 ** Math.floor(Math.log10(rough));
  const multiplier =
    NICE_STEPS.find((step) => step * power >= rough) ?? NICE_STEPS[4] ?? 10;

  return multiplier * power;
};

/**
 * Round ticks that cover every value from `low` to `high`. The step is 1, 2,
 * 2.5 or 5 times a power of ten, and never under `minStep`. A flat
 * line gets a range around its value, so it is drawn in the middle and not
 * along an edge. Where `minStep` is given, any range narrower than it is
 * centred with `minStep` on each side, so sensor noise is not stretched to the
 * full height.
 */
export const valueScale = (
  low: number,
  high: number,
  count = 5,
  minStep = 0,
): ValueScale => {
  if (!Number.isFinite(low) || !Number.isFinite(high)) {
    return { low: 0, high: 1, ticks: [0, 1] };
  }

  let from = Math.min(low, high);
  let to = Math.max(low, high);
  if (minStep > 0 && to - from < minStep) {
    const middle = (from + to) / 2;
    from = middle - minStep;
    to = middle + minStep;
  } else if (from === to) {
    const margin = from === 0 ? 1 : Math.abs(from) / 10;
    from -= margin;
    to += margin;
  }

  const step = Math.max(niceStep(to - from, count), minStep);
  const first = Math.floor(tidy(from / step));
  const last = Math.ceil(tidy(to / step));
  const ticks: number[] = [];
  for (let index = first; index <= last; index += 1) {
    ticks.push(tidy(index * step));
  }

  return {
    low: ticks[0] ?? from,
    high: ticks[ticks.length - 1] ?? to,
    ticks,
  };
};

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

/** The steps a time axis may take, shortest first. */
const TIME_STEPS = [
  MINUTE,
  2 * MINUTE,
  5 * MINUTE,
  10 * MINUTE,
  15 * MINUTE,
  30 * MINUTE,
  HOUR,
  2 * HOUR,
  3 * HOUR,
  6 * HOUR,
  12 * HOUR,
  DAY,
  2 * DAY,
  7 * DAY,
];

/** A time axis: the instants it labels, and whether a label names the day. */
export interface TimeTick {
  readonly at: number;
  /** True where the label is a date: a step of a day or more, or midnight. */
  readonly day: boolean;
}

const isMidnight = (at: number): boolean => {
  const when = new Date(at);

  return when.getHours() === 0 && when.getMinutes() === 0;
};

// The first instant of the step's grid at or after `from`, on the local clock.
// A step under an hour is counted in absolute time, so the hour a clock
// repeats when summer time ends still gets its ticks. A longer step is counted
// on the clock face, so a 3-hour step stays on 0:00, 3:00 and 6:00 across a
// change of summer time.
const ticksFrom = (from: number, to: number, step: number): number[] => {
  const start = new Date(from);
  const out: number[] = [];

  if (step <= HOUR) {
    start.setSeconds(0, 0);
    const minutes = step / MINUTE;
    const offset = start.getMinutes() % minutes;
    let at = start.getTime() - offset * MINUTE;
    if (at < from) at += step;
    for (; at <= to; at += step) out.push(at);

    return out;
  }

  const hours = step / HOUR;
  const year = start.getFullYear();
  const month = start.getMonth();
  const date = start.getDate();
  for (let index = 0; ; index += 1) {
    const at =
      step < DAY
        ? new Date(year, month, date, index * hours).getTime()
        : new Date(year, month, date + index * (step / DAY)).getTime();
    if (at > to) break;
    if (at >= from) out.push(at);
  }

  return out;
};

/**
 * Ticks on the local clock for the instants from `from` to `to`, at most
 * `count` of them where the steps allow it. An empty or reversed range has
 * none.
 */
export const timeTicks = (
  from: number,
  to: number,
  count = 6,
): readonly TimeTick[] => {
  if (!Number.isFinite(from) || !Number.isFinite(to) || to <= from) return [];

  const span = to - from;
  const step =
    TIME_STEPS.find((one) => span / one <= count) ??
    Math.ceil(span / count / (7 * DAY)) * 7 * DAY;

  return ticksFrom(from, to, step).map((at) => ({
    at,
    day: step >= DAY || isMidnight(at),
  }));
};
