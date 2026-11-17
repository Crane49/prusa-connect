import { flushSync } from 'svelte';
import { describe, expect, it } from 'vitest';

import { bind } from 'fractal-client';

import { empty, json, problem, type Routes, stubClient } from '../../test/stub';
import { FractalShares } from './fractal-shares.svelte';

const MOUNT = '/api/v1/modules/com.github.aux.prusa3d.fractal-link';
const LIST = `GET ${MOUNT}/files/shares`;

const storeOver = (routes: Routes) => {
  const stub = stubClient(routes);

  return { stub, store: new FractalShares(bind(stub.client)) };
};

const listing = (items: { storage: string; path: string }[]) =>
  json(200, { items });

describe('FractalShares', () => {
  it('knows nothing before a read answers', () => {
    const { store } = storeOver({});

    expect(store.on).toBeUndefined();
    expect(store.items).toEqual([]);
    expect(store.sharedRoot('local', 'projects')).toBeUndefined();
  });

  it('reads the shared folders once for two callers', async () => {
    const { stub, store } = storeOver({
      [LIST]: listing([{ storage: 'local', path: 'projects' }]),
    });

    await Promise.all([store.load(), store.load()]);

    expect(stub.count(LIST)).toBe(1);
    expect(store.on).toBe(true);
    expect(store.items).toEqual([{ storage: 'local', path: 'projects' }]);
  });

  it('says a server without file sharing is off, which is not a fault', async () => {
    const { store } = storeOver({
      [LIST]: problem(404, 'com.prusa.fractal/FILES_OFF'),
    });

    await store.load();

    expect(store.on).toBe(false);
    expect(store.error).toBeUndefined();
  });

  it('keeps a refusal of the read', async () => {
    const { store } = storeOver({
      [LIST]: problem(403, 'core.authz/FORBIDDEN'),
    });

    await store.load();

    expect(store.on).toBeUndefined();
    expect(store.error).toBeDefined();
  });

  it('names the shared folder that holds a path', async () => {
    const { store } = storeOver({
      [LIST]: listing([
        { storage: 'local', path: 'projects' },
        { storage: 'local', path: 'projects/fixtures' },
        { storage: 'usb', path: 'jobs' },
      ]),
    });
    await store.load();

    expect(store.sharedRoot('local', 'projects')).toBe('projects');
    // A folder shared inside a shared one is its own root, so it can be unshared.
    expect(store.sharedRoot('local', 'projects/fixtures')).toBe(
      'projects/fixtures',
    );
    expect(store.sharedRoot('local', 'projects/other')).toBe('projects');
    expect(store.sharedRoot('local', 'projectsX')).toBeUndefined();
    expect(store.sharedRoot('local', 'jobs')).toBeUndefined();
    expect(store.sharedRoot('usb', 'jobs/benchy')).toBe('jobs');
  });

  it('shares a folder by its path, then reads the list again', async () => {
    let shared = false;
    const { stub, store } = storeOver({
      [LIST]: () =>
        listing(
          shared ? [{ storage: 'local', path: 'projects/fixtures' }] : [],
        ),
      [`PUT ${MOUNT}/files/shares/local/projects/fixtures`]: () => {
        shared = true;

        return empty(204);
      },
    });
    await store.load();

    await store.share('local', 'projects/fixtures', true);

    expect(stub.count(LIST)).toBe(2);
    expect(store.sharedRoot('local', 'projects/fixtures')).toBe(
      'projects/fixtures',
    );
  });

  it('stops sharing a folder', async () => {
    const { stub, store } = storeOver({
      [LIST]: listing([]),
      [`DELETE ${MOUNT}/files/shares/local/projects`]: empty(204),
    });

    await store.share('local', 'projects', false);

    expect(stub.count(`DELETE ${MOUNT}/files/shares/local/projects`)).toBe(1);
  });

  it('throws a refused share and keeps the list it had', async () => {
    const { store } = storeOver({
      [LIST]: listing([]),
      [`PUT ${MOUNT}/files/shares/local/projects`]: problem(
        403,
        'core.authz/FORBIDDEN',
      ),
    });
    await store.load();

    await expect(store.share('local', 'projects', true)).rejects.toThrow();
    expect(store.items).toEqual([]);
  });

  it('forgets the list on a reset, and drops a read that was on its way', async () => {
    let answer: (response: Response) => void = () => undefined;
    const { store } = storeOver({
      [LIST]: () =>
        new Promise<Response>((resolve) => {
          answer = resolve;
        }),
    });

    const read = store.load();
    store.reset();
    answer(listing([{ storage: 'local', path: 'projects' }]));
    await read;

    expect(store.on).toBeUndefined();
    expect(store.items).toEqual([]);
  });
});

describe('FractalShares when answers cross', () => {
  it('keeps the list a share read, when an older read answers after it', async () => {
    const answers: ((response: Response) => void)[] = [];
    const { store } = storeOver({
      [LIST]: () =>
        new Promise<Response>((resolve) => {
          answers.push(resolve);
        }),
      [`PUT ${MOUNT}/files/shares/local/projects`]: empty(204),
    });

    const opening = store.load();
    const sharing = store.share('local', 'projects', true);
    await expect.poll(() => answers.length).toBe(2);
    answers[1]?.(listing([{ storage: 'local', path: 'projects' }]));
    await sharing;
    answers[0]?.(listing([]));
    await opening;

    expect(store.sharedRoot('local', 'projects')).toBe('projects');
  });

  it('keeps the last list when a read fails, with the fault beside it', async () => {
    let fail = false;
    const { store } = storeOver({
      [LIST]: () =>
        fail
          ? problem(503, 'core.internal/RESOURCE_NOT_READY')
          : listing([{ storage: 'local', path: 'projects' }]),
    });
    await store.load();

    fail = true;
    await store.load();

    expect(store.error).toBeDefined();
    expect(store.on).toBe(true);
    expect(store.sharedRoot('local', 'projects')).toBe('projects');
  });

  it('replaces the list once per read, and not on a refused share', async () => {
    const { store } = storeOver({
      [LIST]: listing([{ storage: 'local', path: 'projects' }]),
      [`PUT ${MOUNT}/files/shares/local/jobs`]: problem(
        403,
        'core.authz/FORBIDDEN',
      ),
    });
    let drawn = 0;
    let seen: unknown;
    const cleanup = $effect.root(() => {
      $effect(() => {
        seen = store.items;
        drawn += 1;
      });
    });
    flushSync();

    await store.load();
    flushSync();
    expect(drawn).toBe(2);

    await expect(store.share('local', 'jobs', true)).rejects.toThrow();
    flushSync();
    expect(drawn).toBe(2);

    expect(seen).toBe(store.items);
    cleanup();
  });
});
