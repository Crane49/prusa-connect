import type { components, PrusaConnect } from '@prusa/connect';

import {
  type Outcome,
  previousRange,
  type SettledJob,
  type StatsRange,
} from '../domain/job-stats';

export const JOB_SETTLED = 'com.prusa.connect.device.job.settled';
export const CONTINUITY_LOST = 'com.prusa.connect.stream.continuity.lost';
/** The most prints the core answers in one page. */
const PAGE = 200;
const OUTCOMES: readonly Outcome[] = [
  'finished',
  'failed',
  'cancelled',
  'unknown',
];

export type JobsReader = Pick<PrusaConnect, 'listJobs'>;

/** One print as the core's jobs read answers it. */
type StoredJob = components['schemas']['DeviceJob'];

const instant = (value: string | undefined) => {
  const ms = value === undefined ? Number.NaN : Date.parse(value);
  return Number.isNaN(ms) ? undefined : ms;
};

/** One ended print as a job, or undefined for a row this module cannot read. */
export const toSettledJob = (row: StoredJob): SettledJob | undefined => {
  const started = instant(row.started);
  const ended = instant(row.ended);
  if (started === undefined || ended === undefined) return undefined;

  return {
    deviceId: row.device_id,
    jobId: row.id,
    // A machine that showed no name for the file leaves it empty, not absent.
    name: row.display_name === '' ? row.path : row.display_name,
    started,
    ended,
    outcome: OUTCOMES.find((known) => known === row.outcome) ?? 'unknown',
    machineState: row.state,
  };
};

/**
 * Every print that ended in the range and in the period before it, which the
 * comparison needs, read from the prints the core keeps. The core keeps every
 * print for as long as it keeps the printer, and answers only the printers the
 * person may see, so this pages to the end and reads nothing else.
 */
export const readSettledJobs = async (
  client: JobsReader,
  range: StatsRange,
  signal: AbortSignal,
): Promise<SettledJob[]> => {
  const query = {
    phase: 'ended' as const,
    from: new Date(previousRange(range).from).toISOString(),
    to: new Date(range.to).toISOString(),
    limit: PAGE,
  };
  const jobs: SettledJob[] = [];
  let cursor = '';
  do {
    const page = await client.listJobs(
      { query: cursor === '' ? query : { ...query, cursor } },
      signal,
    );
    for (const row of page.items ?? []) {
      const job = toSettledJob(row);
      if (job !== undefined) jobs.push(job);
    }
    cursor = page.cursor;
  } while (cursor !== '');
  return jobs;
};
