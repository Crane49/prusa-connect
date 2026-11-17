import { describe, expect, it } from 'vitest';

import { type DeviceJobs, Fleet } from '../../data';
import { fakeFleetClient } from '../../test/fakes';
import type { JobRecord, JobsSource } from './types';

/** One print, exactly as the core's jobs operation answers it. */
type CoreJob = NonNullable<DeviceJobs['current']>;

describe('the jobs port', () => {
  it('draws the row the core already answers', () => {
    const answered: CoreJob = {
      id: '20f4af05-458d-42aa-86f9-49329df5a6ef',
      device_id: 'prn-core',
      device_job_id: '17',
      display_name: 'Bracket',
      path: '/usb/examples/bracket.bgcode',
      progress: 100,
      started: '2026-08-26T08:14:05Z',
      ended: '2026-08-26T09:04:05Z',
      state: 'FINISHED',
      time_remaining_ms: 0,
    };

    // Structural typing makes the match silent in both directions, so it is
    // asserted rather than assumed: a field the core renames fails here.
    const record: JobRecord = answered;

    expect(record.display_name).toBe('Bracket');
  });

  it('takes its read state from the store this product hands in', () => {
    const { client } = fakeFleetClient([]);
    const state: Pick<JobsSource, 'error' | 'loaded' | 'loading'> = new Fleet(
      client,
    );

    expect(state.loaded).toBe(false);
    expect(state.error).toBeUndefined();
  });
});
