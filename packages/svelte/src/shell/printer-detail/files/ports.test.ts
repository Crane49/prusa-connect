import { describe, expect, it } from 'vitest';

import { DeviceDetail } from '../../../data';
import type { DeviceDetailClient } from '../../../data/services/device';
import type { PrinterFilesSource } from './types';

const client: DeviceDetailClient = {
  url: '',
  getDevice: () => {
    throw new Error('this fake answers nothing');
  },
  listDeviceJobs: () => Promise.resolve({ items: [], cursor: '', limit: 0 }),
  getDeviceJobs: () => Promise.resolve({}),
  listDeviceStorages: () => Promise.resolve({ items: [], reported: false }),
  listDeviceFiles: () =>
    Promise.resolve({
      directory: {
        folder: true,
        name: 'usb',
        path: 'usb',
        read_only: false,
        size: 0,
      },
      items: [],
    }),
};

describe('the printer files port', () => {
  it('is satisfied by the store this product hands in', () => {
    // Structural typing makes the match silent in both directions, so it is
    // asserted rather than assumed.
    const source: PrinterFilesSource = new DeviceDetail(client, 'printer-1');

    expect(source.filesLoaded).toBe(false);
    expect(source.directory).toBe('');
  });
});
