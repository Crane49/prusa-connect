import { describe, expect, it } from 'vitest';

import { type JobsReader, readSettledJobs, toSettledJob } from './jobs';

const row = (over: Record<string, unknown> = {}) => ({
  id: 'job-1',
  device_job_id: '1',
  device_id: 'a',
  path: 'usb/benchy.bgcode',
  display_name: 'benchy.bgcode',
  state: 'FINISHED',
  progress: 100,
  time_remaining_ms: 0,
  started: '2026-09-22T10:00:00Z',
  ended: '2026-09-22T11:00:00Z',
  outcome: 'finished',
  ...over,
});
const range = {
  from: Date.parse('2026-09-21T00:00:00Z'),
  to: Date.parse('2026-09-28T00:00:00Z'),
};

/** A client answering the pages it was given, in order, and noting each query. */
const reader = (pages: { items: unknown[]; cursor: string }[]) => {
  const queries: Record<string, unknown>[] = [];
  const client = {
    listJobs: (request: { query: Record<string, unknown> }) => {
      queries.push(request.query);
      return Promise.resolve(pages[queries.length - 1]);
    },
  } as unknown as JobsReader;
  return { client, queries };
};

describe('toSettledJob', () => {
  it('reads a stored print, and its outcome', () => {
    expect(toSettledJob(row() as never)).toEqual({
      deviceId: 'a',
      jobId: 'job-1',
      name: 'benchy.bgcode',
      started: Date.parse('2026-09-22T10:00:00Z'),
      ended: Date.parse('2026-09-22T11:00:00Z'),
      outcome: 'finished',
      machineState: 'FINISHED',
    });
  });

  it('reads a print with no outcome, or one it does not know, as unknown', () => {
    expect(toSettledJob(row({ outcome: undefined }) as never)?.outcome).toBe(
      'unknown',
    );
    expect(toSettledJob(row({ outcome: 'exploded' }) as never)?.outcome).toBe(
      'unknown',
    );
  });

  it('names a print by its path when the machine showed no name', () => {
    expect(toSettledJob(row({ display_name: '' }) as never)?.name).toBe(
      'usb/benchy.bgcode',
    );
  });

  it('leaves out a print with no readable end', () => {
    expect(toSettledJob(row({ ended: undefined }) as never)).toBeUndefined();
  });
});

describe('readSettledJobs', () => {
  it('asks for the ended prints of the period and the one before, page after page', async () => {
    const { client, queries } = reader([
      { items: [row()], cursor: 'next' },
      { items: [row({ id: 'job-2' })], cursor: '' },
    ]);
    const jobs = await readSettledJobs(
      client,
      range,
      new AbortController().signal,
    );
    expect(jobs.map((job) => job.jobId)).toEqual(['job-1', 'job-2']);
    expect(queries).toEqual([
      {
        phase: 'ended',
        from: '2026-09-14T00:00:00.000Z',
        to: '2026-09-28T00:00:00.000Z',
        limit: 200,
      },
      {
        phase: 'ended',
        from: '2026-09-14T00:00:00.000Z',
        to: '2026-09-28T00:00:00.000Z',
        limit: 200,
        cursor: 'next',
      },
    ]);
  });
});
