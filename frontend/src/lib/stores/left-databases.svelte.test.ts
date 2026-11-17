// The databases extensions left behind: one read replaced whole, and a purge
// that sends what the person typed and reads the list again.
import { ApiError } from '@prusa/connect';
import { describe, expect, it, vi } from 'vitest';

import {
  instanceParts,
  type LeftDatabase,
  LeftDatabases,
  type LeftDatabasesClient,
} from './left-databases.svelte';

const left: LeftDatabase = {
  instance: 'sink:notes',
  type: 'com.github.aux.acme.notes/notes',
  engine: 'postgres',
  where: 'db.lan:5432/module_notes',
  provisioned: true,
  left_at: '2026-10-21T09:30:00Z',
  why: 'removed',
  purge: 'drop',
};

const clientWith = (
  overrides: Partial<LeftDatabasesClient> = {},
): LeftDatabasesClient => ({
  listModuleDatabases: vi.fn().mockResolvedValue({ items: [left] }),
  purgeModuleDatabase: vi.fn().mockResolvedValue({
    items: [
      {
        instance: 'sink:notes',
        engine: 'postgres',
        where: left.where,
        done: 'dropped',
      },
    ],
  }),
  ...overrides,
});

describe('LeftDatabases', () => {
  it('reads the list once for two loads at the same time', async () => {
    const client = clientWith();
    const store = new LeftDatabases(client);

    await Promise.all([store.load(), store.load()]);

    expect(client.listModuleDatabases).toHaveBeenCalledTimes(1);
    expect(store.items).toEqual([left]);
    expect(store.loaded).toBe(true);
  });

  it('purges with what the person typed and reads the list again', async () => {
    const list = vi
      .fn()
      .mockResolvedValueOnce({ items: [left] })
      .mockResolvedValueOnce({ items: [] });
    const client = clientWith({ listModuleDatabases: list });
    const store = new LeftDatabases(client);
    await store.load();

    expect(await store.purge('sink:notes', 'sink:notes')).toBe(true);

    expect(client.purgeModuleDatabase).toHaveBeenCalledWith({
      path: { kind: 'sink', name: 'notes' },
      query: { confirm: 'sink:notes' },
    });
    expect(store.items).toEqual([]);
    expect(store.purged[0]?.done).toBe('dropped');
    expect(store.purging).toBeUndefined();
  });

  it('keeps a refused purge as its fault and the list as it was', async () => {
    const refused = new ApiError(400, {
      code: 'core.platform/REQUEST_INVALID',
      detail: 'confirm repeats the instance as sink:notes',
      params: {},
    } as unknown as ConstructorParameters<typeof ApiError>[1]);
    const client = clientWith({
      purgeModuleDatabase: vi.fn().mockRejectedValue(refused),
    });
    const store = new LeftDatabases(client);
    await store.load();

    expect(await store.purge('sink:notes', 'notes')).toBe(false);

    expect(store.purgeError).toBe(refused);
    expect(store.items).toEqual([left]);
  });

  it('reads kind:name and refuses anything else', () => {
    expect(instanceParts('sink:notes')).toEqual({
      kind: 'sink',
      name: 'notes',
    });
    expect(instanceParts('notes')).toBeUndefined();
    expect(instanceParts('sink:')).toBeUndefined();
  });
});
