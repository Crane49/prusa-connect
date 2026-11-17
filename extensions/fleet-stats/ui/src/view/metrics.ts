// How the module writes its numbers, shared by the page and the dashboard
// cards so a success rate reads the same wherever it is shown.
import {
  formatDuration,
  formatNumber,
  type Translate,
} from '@prusa/connect-ui/i18n';

import type { Kpis } from '../domain/job-stats';

export const percentOf = (
  value: number,
  options?: Intl.NumberFormatOptions,
): string =>
  formatNumber(value, {
    style: 'percent',
    maximumFractionDigits: 0,
    ...options,
  });

export const rateOf = (t: Translate, value: number | undefined): string =>
  value === undefined ? t('fleet-stats.none') : percentOf(value);

export const durationOf = (t: Translate, ms: number | undefined): string =>
  ms === undefined ? t('fleet-stats.none') : formatDuration(ms / 1000);

export const hoursOf = (hours: number): string =>
  formatNumber(hours, { maximumFractionDigits: 1 });

export interface Metric {
  key: keyof Kpis;
  /** The catalogue key of its label. */
  label: string;
  show: (k: Kpis) => string;
  delta: (now: Kpis, before: Kpis) => number | undefined;
  /** The smallest step the reading is shown in, as a power of ten. */
  step: number;
  format: Intl.NumberFormatOptions;
}

const signed = { signDisplay: 'exceptZero' } as const;

/** The five figures the page leads with, in the order it shows them. */
export const metricsFor = (t: Translate): Metric[] => [
  {
    key: 'jobs',
    label: 'fleet-stats.kpi.jobs',
    show: (k) => formatNumber(k.jobs),
    delta: (n, b) => n.jobs - b.jobs,
    step: 1,
    format: { ...signed, maximumFractionDigits: 0 },
  },
  {
    key: 'successRate',
    label: 'fleet-stats.kpi.success',
    show: (k) => rateOf(t, k.successRate),
    delta: (n, b) =>
      n.successRate === undefined || b.successRate === undefined
        ? undefined
        : n.successRate - b.successRate,
    step: 0.01,
    format: { ...signed, style: 'percent', maximumFractionDigits: 0 },
  },
  {
    key: 'printHours',
    label: 'fleet-stats.kpi.hours',
    show: (k) => hoursOf(k.printHours),
    delta: (n, b) => n.printHours - b.printHours,
    step: 0.1,
    format: { ...signed, maximumFractionDigits: 1 },
  },
  {
    key: 'utilization',
    label: 'fleet-stats.kpi.utilization',
    show: (k) => percentOf(k.utilization),
    delta: (n, b) => n.utilization - b.utilization,
    step: 0.01,
    format: { ...signed, style: 'percent', maximumFractionDigits: 0 },
  },
  {
    key: 'averageJobMs',
    label: 'fleet-stats.kpi.average',
    show: (k) => durationOf(t, k.averageJobMs),
    delta: () => undefined,
    step: 1,
    format: {},
  },
];

/** The change as it is shown: up, down, or flat when it rounds to nothing. */
export const comparison = (
  t: Translate,
  metric: Metric,
  now: Kpis,
  before: Kpis | undefined,
): { trend: 'up' | 'down' | 'flat'; label: string } | undefined => {
  const delta = before && metric.delta(now, before);
  if (delta === undefined) return undefined;
  const rounded = Math.round(delta / metric.step) * metric.step;
  const trend = rounded > 0 ? 'up' : 'down';
  return {
    trend: rounded === 0 ? 'flat' : trend,
    label: t('fleet-stats.kpi.versus', {
      delta: formatNumber(rounded, metric.format),
    }),
  };
};
