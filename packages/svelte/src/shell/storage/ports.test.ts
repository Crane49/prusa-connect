import { describe, expect, it } from 'vitest';

import { Storage } from '../../data';
import type { StorageClient } from '../../data/services/storage';
import type { StorageSource } from './types';

const client: StorageClient = {
  url: 'https://printer.example',
  listStorages: () => Promise.resolve({ items: [] }),
  statStorageEntry: () =>
    Promise.resolve({
      folder: false,
      modified: '2026-09-22T00:00:00Z',
      name: '',
      path: '',
      size: 0,
    }),
  listStorageFiles: () => Promise.resolve({ items: [] }),
  uploadStorageObject: () => Promise.resolve({ bytes: 0, path: '' }),
  deleteStorageEntry: () => Promise.resolve(),
  createStorageFolder: () => Promise.resolve({ path: '' }),
  moveStorageEntry: () => Promise.resolve({ path: '' }),
};

describe('the storage port', () => {
  it('is satisfied by the store this product hands in', () => {
    // Structural typing makes the match silent in both directions, so it is
    // asserted rather than assumed.
    const source: StorageSource = new Storage(client);

    expect(source.loaded).toBe(false);
    expect(source.storage).toBe('');
  });
});
