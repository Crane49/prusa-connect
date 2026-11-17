// The trend as the line chart draws it: one point a day, at the day's start.
import type { TrendDay } from '../domain/job-stats';

export const TREND_METRICS = ['jobs', 'success', 'hours'] as const;
export type TrendMetric = (typeof TREND_METRICS)[number];

/** A metric read from a card's settings or the page, else jobs. */
export const trendMetricOf = (value: unknown): TrendMetric =>
  TREND_METRICS.find((metric) => metric === value) ?? 'jobs';

/** The unit the chart's axis carries for a metric; jobs are a bare count. */
export const unitOf = (metric: TrendMetric): string | undefined => {
  if (metric === 'success') return '%';
  if (metric === 'hours') return 'h';
  return undefined;
};

/** The points of one metric. A day with no decided job has no success rate, and no point. */
export const pointsOf = (
  trend: readonly TrendDay[],
  metric: TrendMetric,
): { at: number; value: number }[] =>
  trend.flatMap((day) => {
    if (metric === 'jobs') return [{ at: day.start, value: day.jobs }];
    if (metric === 'hours') return [{ at: day.start, value: day.printHours }];
    return day.successRate === undefined
      ? []
      : [{ at: day.start, value: day.successRate * 100 }];
  });
