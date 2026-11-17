import { describe, expect, it } from 'vitest';

import { JobDetail, type JobDetailClient } from './job-detail.svelte';
import type { PrintJob } from './services/jobs';

const DEVICE = '34e039fc-13fd-4f4a-b16b-92392643f63a';
const JOB = '0199e3a4-5bb5-7aac-8a2c-9c3982e6373f';

const finished: PrintJob = {
  device_id: DEVICE,
  device_job_id: '12',
  display_name: 'plate.bgcode',
  ended: '2026-09-11T09:04:05Z',
  id: JOB,
  path: '/usb/plate.bgcode',
  progress: 100,
  started: '2026-09-11T08:14:05Z',
  state: 'FINISHED',
  time_remaining_ms: 0,
};

/** A refusal as the SDK throws it: an error carrying the core's code. */
const refusal = (code: string, status: number): Error =>
  Object.assign(new Error(code), { code, status });

/**
 * A client that answers one read by print id. `asked` is every id it was
 * asked for, which is what the request-count assertions read.
 */
const fakeClient = (
  answer: () => Promise<PrintJob>,
): { client: JobDetailClient; asked: string[] } => {
  const asked: string[] = [];
  const client: JobDetailClient = {
    getJob: (request) => {
      asked.push(request.path.job_id);

      return answer();
    },
  };

  return { client, asked };
};

describe('JobDetail', () => {
  it('starts unread, with no print and no refusal', () => {
    const { client } = fakeClient(() => Promise.resolve(finished));
    const store = new JobDetail(client, JOB);

    expect(store.loaded).toBe(false);
    expect(store.job).toBeUndefined();
    expect(store.error).toBeUndefined();
  });

  it('reads the print by its id in one request', async () => {
    const { client, asked } = fakeClient(() => Promise.resolve(finished));
    const store = new JobDetail(client, JOB);
    await store.load();

    expect(store.loaded).toBe(true);
    expect(store.job).toEqual(finished);
    expect(asked).toEqual([JOB]);
  });

  it('reads a print the server does not hold as missing, not as a refusal', async () => {
    const { client } = fakeClient(() =>
      Promise.reject(refusal('core.printer/JOB_NOT_FOUND', 404)),
    );
    const store = new JobDetail(client, JOB);
    await store.load();

    expect(store.loaded).toBe(true);
    expect(store.job).toBeUndefined();
    expect(store.error).toBeUndefined();
  });

  it('keeps any other refusal as its code and stays unread', async () => {
    const { client } = fakeClient(() =>
      Promise.reject(refusal('core.auth/UNAUTHENTICATED', 401)),
    );
    const store = new JobDetail(client, JOB);
    await store.load();

    expect(store.error?.code).toBe('core.auth/UNAUTHENTICATED');
    expect(store.loaded).toBe(false);
    expect(store.job).toBeUndefined();
  });

  it('hands a second call the read already in flight', async () => {
    const { client, asked } = fakeClient(() => Promise.resolve(finished));
    const store = new JobDetail(client, JOB);
    const first = store.load();
    const second = store.load();

    expect(second).toBe(first);
    await first;
    expect(asked).toHaveLength(1);
  });
});
