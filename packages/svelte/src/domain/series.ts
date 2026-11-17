// A series of readings, and the two things done to one before it is drawn.
//
// Both functions are pure and neither knows where its points came from. A
// stored point read over HTTP and a live one taken off the event stream are
// the same shape by the time they reach here.

/** One point of a series: when it was measured, and what it read. */
export interface TelemetryPoint {
  /** Milliseconds since the epoch, as the origin measured it. */
  readonly at: number;
  readonly value: number;
}

/**
 * One series from two.
 *
 * A point present in both sides is kept once and the stored one wins. The
 * stored point carries the instant the origin measured, which is the instant
 * the whole series is spelled in, so preferring it keeps one clock across the
 * join.
 */
export const mergeSeries = (
  stored: readonly TelemetryPoint[],
  live: readonly TelemetryPoint[],
): readonly TelemetryPoint[] => {
  if (live.length === 0) return [...stored].sort(byInstant);
  if (stored.length === 0) return [...live].sort(byInstant);

  const kept = new Map<number, TelemetryPoint>();
  for (const point of live) kept.set(point.at, point);
  for (const point of stored) kept.set(point.at, point);

  return [...kept.values()].sort(byInstant);
};

const byInstant = (one: TelemetryPoint, other: TelemetryPoint): number =>
  one.at - other.at;

/**
 * A series reduced to what a chart can draw.
 *
 * A printer reports several times a second, and a chart is some hundreds of
 * pixels wide, so two hours of one metric carries far more points than it has
 * room for. The window is cut into buckets and each bucket keeps its first
 * point, its last, its highest and its lowest.
 *
 * Keeping the extremes is the whole point. A mean, or every fourth point,
 * would drop the temperature spike that is the reason somebody opened the
 * chart, and the line would look calm where the printer was not.
 */
export const thinSeries = (
  points: readonly TelemetryPoint[],
  budget: number,
): readonly TelemetryPoint[] => {
  // Four points leave each bucket, so the bucket count is a quarter of the
  // budget. Two buckets is the floor below which this is not a reduction.
  const buckets = Math.max(2, Math.floor(budget / 4));
  if (points.length <= buckets * 4) return points;

  const size = Math.ceil(points.length / buckets);
  const kept = new Map<number, TelemetryPoint>();

  for (let start = 0; start < points.length; start += size) {
    const bucket = points.slice(start, start + size);

    let first: TelemetryPoint | undefined;
    let last: TelemetryPoint | undefined;
    let lowest: TelemetryPoint | undefined;
    let highest: TelemetryPoint | undefined;

    for (const point of bucket) {
      first ??= point;
      last = point;
      if (lowest === undefined || point.value < lowest.value) lowest = point;
      if (highest === undefined || point.value > highest.value) highest = point;
    }

    // `size` is at least one and the loop above runs while `start` is still
    // inside `points`, so a bucket is never empty and all four are set.
    // Keyed by instant, so a bucket whose first point is also its highest
    // contributes that point once.
    for (const point of [first, lowest, highest, last]) {
      if (point !== undefined) kept.set(point.at, point);
    }
  }

  return [...kept.values()].sort(byInstant);
};
