import { describe, expect, it } from 'vitest';

import { JobDetail } from '../../data';
import type { JobDetailClient } from '../../data/job-detail.svelte';
import type { JobSource } from './types';

const client: JobDetailClient = {
  getJob: () => {
    throw new Error('this fake answers nothing');
  },
};

describe('the job port', () => {
  it('is satisfied by the store this product hands in', () => {
    // Structural typing makes the match silent in both directions, so it is
    // asserted rather than assumed.
    const source: JobSource = new JobDetail(
      client,
      '0199e3a4-5bb5-7aac-8a2c-9c3982e6373f',
    );

    expect(source.loaded).toBe(false);
    expect(source.job).toBeUndefined();
    expect(source.error).toBeUndefined();
  });
});
