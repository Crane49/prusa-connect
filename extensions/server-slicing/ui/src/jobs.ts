// The Jobs tab as one listing: what a row shows of a job, and how the reader
// narrows and orders the jobs. Plain functions, so a test reads them without a
// browser.
import type { Job } from './api';

/** The project's file name: the upload's, or the last part of its path. */
export const projectOf = (job: Job): string =>
  job.source.upload ?? job.source.path?.split('/').at(-1) ?? '';

/** The slicer that ran the job, or the one it asked for. */
export const slicerOf = (job: Job): string =>
  job.result?.slicer ?? job.slicer ?? '';

/** The G-code's file name, when the job wrote one. */
export const outputOf = (job: Job): string | undefined =>
  job.result?.path.split('/').at(-1);

/** How long the job ran, in seconds, or undefined before it started. */
export const durationOf = (job: Job, now = Date.now()): number | undefined => {
  if (!job.started_at) return undefined;
  const end = job.finished_at ? Date.parse(job.finished_at) : now;

  return Math.max(1, Math.round((end - Date.parse(job.started_at)) / 1000));
};

/** Whether the job can still be canceled. */
export const isActive = (job: Job): boolean =>
  job.state === 'queued' || job.state === 'running';

/** The address of a file in the app's storage screen. */
export const fileHref = (storage: string, path: string): string =>
  `/storage/${encodeURIComponent(storage)}/file/${encodeURIComponent(path)}`;

export type JobSortColumn = 'created' | 'duration';

export interface JobSort {
  column: JobSortColumn;
  ascending: boolean;
}

export const DEFAULT_JOB_SORT: JobSort = {
  column: 'created',
  ascending: false,
};

export const compareJobs = (a: Job, b: Job, sort: JobSort): number => {
  const step =
    sort.column === 'duration'
      ? (durationOf(a) ?? -1) - (durationOf(b) ?? -1)
      : a.created_at.localeCompare(b.created_at);

  return sort.ascending ? step : -step;
};

export interface JobFilter {
  q: string;
  state: readonly string[];
  slicer: readonly string[];
  project: readonly string[];
}

export const NO_JOB_FILTER: JobFilter = {
  q: '',
  state: [],
  slicer: [],
  project: [],
};

export type JobFacet = 'state' | 'slicer' | 'project';

export const JOB_STATES: readonly Job['state'][] = [
  'running',
  'queued',
  'done',
  'failed',
  'canceled',
];

const valueOf = (job: Job, facet: JobFacet): string => {
  if (facet === 'state') return job.state;
  if (facet === 'slicer') return slicerOf(job);

  return projectOf(job);
};

const matches = (job: Job, filter: JobFilter, skip?: JobFacet): boolean => {
  for (const facet of ['state', 'slicer', 'project'] as const) {
    if (facet === skip) continue;
    const chosen = filter[facet];
    if (chosen.length > 0 && !chosen.includes(valueOf(job, facet))) {
      return false;
    }
  }

  const term = filter.q.trim().toLowerCase();
  if (term === '') return true;

  return [
    job.id,
    job.by,
    projectOf(job),
    job.source.path,
    job.result?.path,
    slicerOf(job),
    job.error?.message,
  ]
    .filter((text): text is string => text !== undefined)
    .some((text) => text.toLowerCase().includes(term));
};

export const filterJobs = (jobs: readonly Job[], filter: JobFilter): Job[] =>
  jobs.filter((job) => matches(job, filter));

export const jobFacetValues = (
  jobs: readonly Job[],
  facet: JobFacet,
): string[] => {
  if (facet === 'state') {
    return JOB_STATES.filter((state) =>
      jobs.some((job) => job.state === state),
    );
  }

  return [...new Set(jobs.map((job) => valueOf(job, facet)))]
    .filter((value) => value !== '')
    .sort((a, b) => a.localeCompare(b));
};

export const jobFacetCounts = (
  jobs: readonly Job[],
  filter: JobFilter,
  facet: JobFacet,
): Map<string, number> => {
  const counts = new Map<string, number>();
  for (const job of jobs) {
    if (!matches(job, filter, facet)) continue;
    const value = valueOf(job, facet);
    counts.set(value, (counts.get(value) ?? 0) + 1);
  }

  return counts;
};
