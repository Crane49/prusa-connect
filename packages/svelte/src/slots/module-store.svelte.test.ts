import { describe, expect, it } from 'vitest';

import { fakeStorage } from '../test/fakes';
import {
  createModuleStore,
  forgetModuleCaches,
  type ModuleStateClient,
  storeKeyPrefix,
} from './module-store.svelte';

describe('ModuleStore', () => {
  it('reads back only what its own namespace wrote', () => {
    const { storage } = fakeStorage({
      [`${storeKeyPrefix('spools')}unit`]: 'grams',
      [`${storeKeyPrefix('other')}unit`]: 'metres',
      unrelated: 'kept',
    });

    const store = createModuleStore('spools', { storage });

    expect(store.keys()).toEqual(['unit']);
    expect(store.get('unit')).toBe('grams');
  });

  it('writes under the namespace prefix and leaves another module alone', () => {
    const { storage, map } = fakeStorage({
      [`${storeKeyPrefix('other')}unit`]: 'metres',
    });
    const store = createModuleStore('spools', { storage });

    store.set('unit', 'grams');

    expect(map.get(`${storeKeyPrefix('spools')}unit`)).toBe('grams');
    expect(map.get(`${storeKeyPrefix('other')}unit`)).toBe('metres');
  });

  it('rejects an empty key, and nothing is persisted', () => {
    const { storage, map } = fakeStorage();
    const store = createModuleStore('spools', { storage });

    store.set('', 'nowhere');

    expect(store.keys()).toEqual([]);
    expect(map.size).toBe(0);
  });

  it('deletes a record from the store and from storage', () => {
    const { storage, map } = fakeStorage();
    const store = createModuleStore('spools', { storage });
    store.set('unit', 'grams');

    store.delete('unit');

    expect(store.get('unit')).toBeUndefined();
    expect(map.size).toBe(0);
  });

  it('remembers nothing when there is no storage', () => {
    const store = createModuleStore('spools', { storage: null });

    store.set('unit', 'grams');

    expect(store.get('unit')).toBe('grams');
    expect(store.namespace).toBe('spools');
  });
});

// Everyone who signs in on one browser shares its `localStorage`.
describe('ModuleStore for two people on one browser', () => {
  it('keeps each person records apart', () => {
    const { storage } = fakeStorage();
    const ada = createModuleStore('spools', { storage, person: 'user:ada' });
    ada.set('unit', 'grams');

    const grace = createModuleStore('spools', {
      storage,
      person: 'user:grace',
    });
    const nobody = createModuleStore('spools', { storage });

    expect(grace.keys()).toEqual([]);
    expect(nobody.keys()).toEqual([]);
    expect(
      createModuleStore('spools', { storage, person: 'user:ada' }).get('unit'),
    ).toBe('grams');
  });

  it('forgets every cached record of every person, and nothing else', () => {
    const { storage, map } = fakeStorage({ unrelated: 'kept' });
    createModuleStore('spools', { storage, person: 'user:ada' }).set(
      'unit',
      'grams',
    );
    createModuleStore('other', { storage, person: 'user:grace' }).set(
      'unit',
      'metres',
    );

    forgetModuleCaches(storage);

    expect([...map.keys()]).toEqual(['unrelated']);
  });

  it('writes nothing to the cache once it is closed', async () => {
    const { storage, map } = fakeStorage();
    let answer: (() => void) | undefined;
    const client: ModuleStateClient = {
      listModuleState: () =>
        new Promise((resolve) => {
          answer = () => {
            resolve({ items: ['unit'] });
          };
        }),
      getModuleState: () => Promise.resolve({ key: 'unit', value: 'grams' }),
      putModuleState: () => Promise.resolve({ key: 'unit', value: 'grams' }),
      deleteModuleState: () => Promise.resolve(),
    };
    const store = createModuleStore('spools', {
      storage,
      client,
      person: 'user:ada',
    });

    store.close();
    answer?.();
    await store.ready();

    expect(map.size).toBe(0);
    expect(store.keys()).toEqual([]);
  });
});

describe('ModuleStore once closed', () => {
  // A closed store belongs to the person who left. A late write from their
  // module would reach the server with the next person's cookie.
  it('sends no write or delete to the server', async () => {
    const calls: string[] = [];
    const client: ModuleStateClient = {
      listModuleState: () => Promise.resolve({ items: ['unit'] }),
      getModuleState: () => Promise.resolve({ key: 'unit', value: 'grams' }),
      putModuleState: () => {
        calls.push('put');

        return Promise.resolve({ key: 'unit', value: 'metres' });
      },
      deleteModuleState: () => {
        calls.push('delete');

        return Promise.resolve();
      },
    };
    const { storage, map } = fakeStorage();
    const store = createModuleStore('spools', {
      storage,
      client,
      person: 'user:ada',
    });
    await store.ready();

    store.close();
    store.set('unit', 'metres');
    store.delete('unit');

    expect(calls).toEqual([]);
    expect(map.size).toBe(1);
  });
});
