// The last hour of a module's memory as the chart draws it: the readings in
// MiB against the budget, a mark where the host stopped the process and a
// shaded span while it was off.
import type {
  ChartMarker,
  ChartRegion,
  ChartThreshold,
} from '@prusa/connect-ui/primitives';
import type { Translate } from '@prusa/connect-ui/i18n';

import type { ResourceMemoryHistory } from '../stores/module-operations.svelte';
import { type ResourceMemory, stopText } from './stops';

const MIB = 1024 * 1024;
const HOUR_MS = 3_600_000;
const KEY = 'app.settings.extensions.history';

export interface MemorySeries {
  /** An unread sample is NaN, a gap in the line, and never 0. */
  readonly points: { at: number; value: number }[];
  readonly markers: ChartMarker[];
  readonly regions: ChartRegion[];
  readonly threshold: ChartThreshold;
  readonly domain: [number, number];
}

export const memorySeries = (
  history: ResourceMemoryHistory,
  memory: ResourceMemory,
  clock: number,
  t: Translate,
): MemorySeries => {
  const points = history.items.map((one) => ({
    at: Date.parse(one.at),
    value: one.value === undefined ? Number.NaN : one.value / MIB,
  }));
  // The readings carry the server's clock, which may run ahead of the browser's.
  const now = Math.max(clock, ...points.map((point) => point.at));
  const stops = history.stops.map((one) => ({
    at: Date.parse(one.at),
    label: stopText(one, t, memory),
  }));
  const off = memory.switched_off !== undefined || memory.held_until_restart;

  return {
    points,
    markers: stops,
    // An off process leaves no samples, so a span ends at the next reading,
    // or at now when none follows or the host keeps the module off.
    regions: stops.map((stop, at) => {
      const next = points.find((point) => point.at > stop.at)?.at;
      const open = next === undefined || (off && at === stops.length - 1);

      return { from: stop.at, to: open ? now : next, label: t(`${KEY}.off`) };
    }),
    threshold: { value: memory.budget_bytes / MIB, label: t(`${KEY}.budget`) },
    domain: [now - HOUR_MS, now],
  };
};
