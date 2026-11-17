/**
 * The fleet's statistics, from settled jobs alone. Pure: no Svelte, no SDK.
 * The output is shaped as a core endpoint would answer it, so moving this to
 * the server changes the caller's data source and nothing else.
 */

export type Outcome = 'finished' | 'failed' | 'cancelled' | 'unknown';

export interface SettledJob {
  deviceId: string;
  jobId: string;
  name: string;
  /** Epoch milliseconds. */
  started: number;
  ended: number;
  outcome: Outcome;
  machineState: string;
}

/** Epoch milliseconds, `from` inclusive, `to` exclusive. */
export interface StatsRange {
  from: number;
  to: number;
}

export interface DeviceInfo {
  id: string;
  name: string;
  model: string;
  /** Compared as they are; the caller spells every tag one way first. */
  tags: readonly string[];
}

/** Which printers a view counts. Empty fields narrow nothing. */
export interface Scope {
  /** These printers alone, by id. */
  printers: readonly string[];
  /** The printers with this tag alone. */
  tag: string;
}

/** Every printer, and the jobs of printers that left the fleet as well. */
export const WHOLE_FLEET: Scope = { printers: [], tag: '' };

export interface Kpis {
  jobs: number;
  /** Finished over finished, failed and cancelled; undefined with none decided. */
  successRate: number | undefined;
  printHours: number;
  /** Printing time over the time every printer in the fleet had. */
  utilization: number;
  averageJobMs: number | undefined;
}

/** `cells[weekday * 24 + hour]`, Monday first, local time, each 0 to 1. */
export interface DeviceUtilization {
  deviceId: string;
  name: string;
  cells: number[];
}

export interface FailureRow {
  key: string;
  label: string;
  jobs: number;
  successRate: number | undefined;
  failed: number;
  cancelled: number;
}

/** One local calendar day of the period. */
export interface TrendDay {
  /** Epoch milliseconds of the day's local midnight. */
  start: number;
  /** Jobs that ended on the day. */
  jobs: number;
  /** Finished over decided; undefined on a day with none decided. */
  successRate: number | undefined;
  /** Printing time that fell on the day, inside the period. */
  printHours: number;
}

/** One file, by the name its jobs printed it under. */
export interface FileRow {
  name: string;
  prints: number;
  finished: number;
  failed: number;
  cancelled: number;
  successRate: number | undefined;
  /** Printing time of every print of it, start to end. */
  printMs: number;
  /** Epoch milliseconds of its latest end. */
  lastEnded: number;
}

export interface StateCount {
  state: string;
  count: number;
}

export interface JobStats {
  kpis: Kpis;
  /** Undefined when the period before it holds no jobs, so there is nothing to compare with. */
  previous: Kpis | undefined;
  utilization: DeviceUtilization[];
  byDevice: FailureRow[];
  byMachineState: StateCount[];
  /** Failed and cancelled jobs, newest first. */
  recentFailures: SettledJob[];
  /** Every local day from the period's first midnight on, oldest first. */
  trend: TrendDay[];
  /** The files printed in the period, most printed first. */
  files: FileRow[];
}

export const UNKNOWN_STATE = 'unknown';

const HOUR_MS = 3_600_000;
const CELLS = 7 * 24;

export const previousRange = ({ from, to }: StatsRange): StatsRange => ({
  from: from - (to - from),
  to: from,
});

const isFailure = (job: SettledJob) =>
  job.outcome === 'failed' || job.outcome === 'cancelled';

const rateOf = (finished: number, decided: number) =>
  decided === 0 ? undefined : finished / decided;

/** Milliseconds of the job inside the range. */
const overlap = (job: SettledJob, range: StatsRange) =>
  Math.max(
    0,
    Math.min(job.ended, range.to) - Math.max(job.started, range.from),
  );

/** The jobs a period reports: those that ended inside it. */
const endedIn = (jobs: readonly SettledJob[], range: StatsRange) =>
  jobs.filter((job) => job.ended >= range.from && job.ended < range.to);

const cellOf = (hourStart: Date) =>
  ((hourStart.getDay() + 6) % 7) * 24 + hourStart.getHours();

/**
 * Walks the local hours that meet [from, to). Each step is a wall-clock hour,
 * so a DST day has 23 or 25 of them and a repeated hour lands in its cell
 * twice.
 */
const eachHour = (
  from: number,
  to: number,
  visit: (cell: number, start: number, end: number) => void,
) => {
  const first = new Date(from);
  // Subtracting the elapsed part of the hour keeps the instant inside a repeated hour.
  let start =
    from -
    (first.getMinutes() * 60_000 +
      first.getSeconds() * 1000 +
      first.getMilliseconds());
  while (start < to) {
    visit(
      cellOf(new Date(start)),
      Math.max(start, from),
      Math.min(start + HOUR_MS, to),
    );
    start += HOUR_MS;
  }
};

const kpisOf = (
  jobs: readonly SettledJob[],
  devices: readonly DeviceInfo[],
  range: StatsRange,
): Kpis => {
  const ended = endedIn(jobs, range);
  const finished = ended.filter((job) => job.outcome === 'finished').length;
  const decided = ended.filter((job) => job.outcome !== 'unknown').length;
  const fleet = new Set(devices.map((device) => device.id));
  const printedMs = jobs.reduce((sum, job) => sum + overlap(job, range), 0);
  const fleetMs = jobs
    .filter((job) => fleet.has(job.deviceId))
    .reduce((sum, job) => sum + overlap(job, range), 0);
  const available = devices.length * (range.to - range.from);
  return {
    jobs: ended.length,
    successRate: rateOf(finished, decided),
    printHours: printedMs / HOUR_MS,
    utilization: available === 0 ? 0 : Math.min(1, fleetMs / available),
    averageJobMs:
      ended.length === 0
        ? undefined
        : ended.reduce((sum, job) => sum + (job.ended - job.started), 0) /
          ended.length,
  };
};

const utilizationOf = (
  jobs: readonly SettledJob[],
  devices: readonly DeviceInfo[],
  range: StatsRange,
): DeviceUtilization[] => {
  const available = new Array<number>(CELLS).fill(0);
  eachHour(range.from, range.to, (cell, start, end) => {
    available[cell] = (available[cell] ?? 0) + (end - start);
  });

  return devices.map((device) => {
    const occupied = new Array<number>(CELLS).fill(0);
    for (const job of jobs) {
      if (job.deviceId !== device.id) continue;
      const from = Math.max(job.started, range.from);
      const to = Math.min(job.ended, range.to);
      if (to <= from) continue;
      eachHour(from, to, (cell, start, end) => {
        occupied[cell] = (occupied[cell] ?? 0) + (end - start);
      });
    }
    return {
      deviceId: device.id,
      name: device.name,
      cells: occupied.map((ms, cell) => {
        const total = available[cell] ?? 0;
        return total === 0 ? 0 : Math.min(1, ms / total);
      }),
    };
  });
};

const failureRows = (
  jobs: readonly SettledJob[],
  keyOf: (job: SettledJob) => string,
  labelOf: (key: string) => string,
) => {
  const rows = new Map<
    string,
    {
      jobs: number;
      finished: number;
      decided: number;
      failed: number;
      cancelled: number;
    }
  >();
  for (const job of jobs) {
    const key = keyOf(job);
    const row = rows.get(key) ?? {
      jobs: 0,
      finished: 0,
      decided: 0,
      failed: 0,
      cancelled: 0,
    };
    row.jobs++;
    if (job.outcome !== 'unknown') row.decided++;
    if (job.outcome === 'finished') row.finished++;
    if (job.outcome === 'failed') row.failed++;
    if (job.outcome === 'cancelled') row.cancelled++;
    rows.set(key, row);
  }
  return [...rows.entries()]
    .map(([key, row]): FailureRow => ({
      key,
      label: labelOf(key),
      jobs: row.jobs,
      successRate: rateOf(row.finished, row.decided),
      failed: row.failed,
      cancelled: row.cancelled,
    }))
    .sort(
      (a, b) =>
        b.failed + b.cancelled - (a.failed + a.cancelled) ||
        a.label.localeCompare(b.label),
    );
};

/** Local midnight of the day `at` falls on. */
const dayStart = (at: number) => {
  const day = new Date(at);
  day.setHours(0, 0, 0, 0);
  return day.getTime();
};

/** Local midnight of the next day; a DST day is 23 or 25 hours long. */
const nextDay = (start: number) => {
  const day = new Date(start);
  day.setDate(day.getDate() + 1);
  return day.getTime();
};

const trendOf = (
  jobs: readonly SettledJob[],
  range: StatsRange,
): TrendDay[] => {
  const days: TrendDay[] = [];
  // The first day is the first whole one: a period that starts mid-day would
  // otherwise lead with a stub of a day that reads as a drop.
  const first = dayStart(range.from);
  for (
    let start = first === range.from ? first : nextDay(first);
    start < range.to;
  ) {
    const end = nextDay(start);
    const inside = {
      from: Math.max(start, range.from),
      to: Math.min(end, range.to),
    };
    const ended = endedIn(jobs, inside);
    const finished = ended.filter((job) => job.outcome === 'finished').length;
    const decided = ended.filter((job) => job.outcome !== 'unknown').length;
    days.push({
      start,
      jobs: ended.length,
      successRate: rateOf(finished, decided),
      printHours:
        jobs.reduce((sum, job) => sum + overlap(job, inside), 0) / HOUR_MS,
    });
    start = end;
  }
  return days;
};

const filesOf = (ended: readonly SettledJob[]): FileRow[] => {
  const rows = new Map<string, FileRow>();
  for (const job of ended) {
    const row = rows.get(job.name) ?? {
      name: job.name,
      prints: 0,
      finished: 0,
      failed: 0,
      cancelled: 0,
      successRate: undefined,
      printMs: 0,
      lastEnded: 0,
    };
    row.prints++;
    if (job.outcome === 'finished') row.finished++;
    if (job.outcome === 'failed') row.failed++;
    if (job.outcome === 'cancelled') row.cancelled++;
    row.printMs += job.ended - job.started;
    row.lastEnded = Math.max(row.lastEnded, job.ended);
    rows.set(job.name, row);
  }
  return [...rows.values()]
    .map((row) => ({
      ...row,
      successRate: rateOf(
        row.finished,
        row.finished + row.failed + row.cancelled,
      ),
    }))
    .sort(
      (a, b) =>
        b.prints - a.prints ||
        b.failed + b.cancelled - (a.failed + a.cancelled) ||
        a.name.localeCompare(b.name),
    );
};

/**
 * The jobs and printers a scope counts. The whole fleet keeps the jobs of
 * printers that are gone; a narrower scope names printers, so it does not.
 */
export const narrow = (
  jobs: readonly SettledJob[],
  devices: readonly DeviceInfo[],
  scope: Scope,
): { jobs: readonly SettledJob[]; devices: readonly DeviceInfo[] } => {
  if (scope.printers.length === 0 && scope.tag === '') return { jobs, devices };
  const chosen = new Set(scope.printers);
  const kept = devices.filter(
    (device) =>
      (chosen.size === 0 || chosen.has(device.id)) &&
      (scope.tag === '' || device.tags.includes(scope.tag)),
  );
  const ids = new Set(kept.map((device) => device.id));
  return { jobs: jobs.filter((job) => ids.has(job.deviceId)), devices: kept };
};

export const summarise = (
  jobs: readonly SettledJob[],
  devices: readonly DeviceInfo[],
  range: StatsRange,
): JobStats => {
  const known = new Map(devices.map((device) => [device.id, device]));
  const ended = endedIn(jobs, range);

  const stateCounts = new Map<string, number>();
  for (const job of ended.filter(isFailure)) {
    const state = job.machineState === '' ? UNKNOWN_STATE : job.machineState;
    stateCounts.set(state, (stateCounts.get(state) ?? 0) + 1);
  }

  const previous = kpisOf(jobs, devices, previousRange(range));

  return {
    kpis: kpisOf(jobs, devices, range),
    previous: previous.jobs > 0 ? previous : undefined,
    utilization: utilizationOf(jobs, devices, range),
    byDevice: failureRows(
      ended,
      (job) => job.deviceId,
      (id) => known.get(id)?.name ?? id,
    ),
    byMachineState: [...stateCounts.entries()]
      .map(([state, count]) => ({ state, count }))
      .sort((a, b) => b.count - a.count || a.state.localeCompare(b.state)),
    recentFailures: ended.filter(isFailure).sort((a, b) => b.ended - a.ended),
    trend: trendOf(jobs, range),
    files: filesOf(ended),
  };
};
