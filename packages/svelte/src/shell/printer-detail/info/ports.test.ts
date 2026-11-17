import { describe, expect, it } from 'vitest';

import { DeviceDetail } from '../../../data';
import type { DeviceDetailClient } from '../../../data/services/device';
import type { PrinterHistorySource } from './types';

const client: DeviceDetailClient = {
  url: '',
  getDevice: () => {
    throw new Error('this fake answers nothing');
  },
  listDeviceJobs: () => Promise.resolve({ items: [], cursor: '', limit: 0 }),
  getDeviceJobs: () => {
    throw new Error('this fake answers nothing');
  },
  listDeviceStorages: () => {
    throw new Error('this fake answers nothing');
  },
  listDeviceFiles: () => {
    throw new Error('this fake answers nothing');
  },
};

describe('the print history port', () => {
  it('is satisfied by the store this product hands in', () => {
    // Structural typing makes the match silent in both directions, so it is
    // asserted rather than assumed.
    const source: PrinterHistorySource = new DeviceDetail(client, 'printer-1');

    expect(source.jobsLoaded).toBe(false);
    expect(source.jobs).toBeUndefined();
    expect(source.jobsError).toBeUndefined();
  });
});
