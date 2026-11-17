import { flushSync } from 'svelte';
import { describe, expect, it } from 'vitest';

import { bind } from 'fractal-client';

import { json, problem, type Routes, stubClient } from '../../test/stub';
import { FractalStores } from './fractal-stores.svelte';

const MOUNT = '/api/v1/modules/com.github.aux.prusa3d.fractal-link';
const STORES = `GET ${MOUNT}/files/stores`;

const ROOT_HQ = 'sha256:aa';
const PROJECTS = 'sha256:bb';
const FIXTURES = 'sha256:cc';
const ROOT_ZAB = 'sha256:dd';

const PROJECTS_AT = { node: 'hq', instance: 'local', path: 'projects' };
const FIXTURES_AT = {
  node: 'hq',
  instance: 'local',
  path: 'projects/fixtures',
};

const contentStore = (
  node: string,
  instance: string,
  rootHash: string | null,
  status: 'OK' | 'STALE' = 'OK',
) => ({
  node,
  interface: 'ContentStore1',
  instance,
  status,
  updated_at: 1_793_700_000_000,
  props: { rootHash, mediaPresent: true, removable: false },
});

const listingOf = (
  node: string,
  identity: string,
  entries: { name: string; kind: 'file' | 'directory'; identity: string }[],
) =>
  json(200, {
    node,
    instance: 'local',
    identity,
    entries: entries.map((entry) => ({ size: 1024, ...entry })),
  });

const tree: Routes = {
  [STORES]: json(200, {
    node: 'hq/brno',
    items: [
      contentStore('hq', 'local', ROOT_HQ),
      // This server's own store is in the copy too, and is not shared with it.
      contentStore('hq/brno', 'local', 'sha256:ee'),
      // Another interface of a node is not a store.
      { ...contentStore('hq', 'one', null), interface: 'Printer1' },
      contentStore('hq/brno/zabovresky', 'local', ROOT_ZAB, 'STALE'),
      // A store whose owner has not scanned it yet has no top listing.
      contentStore('hq', 'usb', null),
    ],
    total: 5,
    truncated: false,
  }),
  [`GET ${MOUNT}/files/stores/hq/local/${ROOT_HQ}`]: listingOf('hq', ROOT_HQ, [
    { name: 'projects', kind: 'directory', identity: PROJECTS },
  ]),
  [`GET ${MOUNT}/files/stores/hq/local/${PROJECTS}`]: listingOf(
    'hq',
    PROJECTS,
    [{ name: 'fixtures', kind: 'directory', identity: FIXTURES }],
  ),
  [`GET ${MOUNT}/files/stores/hq/local/${FIXTURES}`]: listingOf(
    'hq',
    FIXTURES,
    [{ name: 'benchy.bgcode', kind: 'file', identity: 'sha256:ff' }],
  ),
  [`GET ${MOUNT}/files/stores/hq/brno/zabovresky/local/${ROOT_ZAB}`]: listingOf(
    'hq/brno/zabovresky',
    ROOT_ZAB,
    [{ name: 'jigs', kind: 'directory', identity: 'sha256:11' }],
  ),
};

const storeOver = (routes: Routes) => {
  const stub = stubClient(routes);

  return { stub, store: new FractalStores(bind(stub.client)) };
};

describe('FractalStores', () => {
  it('has read nothing before load', () => {
    const { store } = storeOver({});

    expect(store.loaded).toBe(false);
    expect(store.stores).toEqual([]);
    expect(store.rows).toEqual([]);
  });

  it('lists the top of every store of the other servers', async () => {
    const { store } = storeOver(tree);

    await store.load();

    expect(store.node).toBe('hq/brno');
    expect(store.stores.map((one) => `${one.node} ${one.instance}`)).toEqual([
      'hq local',
      'hq/brno/zabovresky local',
      'hq usb',
    ]);
    expect(store.rows.map((row) => `${row.store.node}:${row.path}`)).toEqual([
      'hq:projects',
      'hq/brno/zabovresky:jigs',
    ]);
  });

  it('says when a server may be out of date', async () => {
    const { store } = storeOver(tree);

    await store.load();

    const [hq, zabovresky] = store.stores;
    expect(hq?.staleSince).toBeUndefined();
    expect(zabovresky?.staleSince).toBe(1_793_700_000_000);
  });

  it('opens a folder, walks into another and back to the top', async () => {
    const { store } = storeOver(tree);
    await store.load();

    await store.goTo(PROJECTS_AT);
    expect(store.rows.map((row) => row.path)).toEqual(['projects/fixtures']);

    await store.goTo(FIXTURES_AT);
    expect(store.trail.map((one) => one.name)).toEqual([
      'projects',
      'fixtures',
    ]);
    expect(store.rows).toMatchObject([
      { path: 'projects/fixtures/benchy.bgcode', folder: false, size: 1024 },
    ]);

    await store.goTo(PROJECTS_AT);
    expect(store.rows.map((row) => row.path)).toEqual(['projects/fixtures']);

    await store.goTo(undefined);
    expect(store.trail).toEqual([]);
    expect(store.rows).toHaveLength(2);
  });

  it('walks to a folder from the top of its store, as an address names it', async () => {
    const { store } = storeOver(tree);
    await store.load();

    await store.goTo(FIXTURES_AT);

    expect(store.place).toEqual(FIXTURES_AT);
    expect(store.trail.map((one) => one.name)).toEqual([
      'projects',
      'fixtures',
    ]);
    expect(store.rows.map((row) => row.name)).toEqual(['benchy.bgcode']);
  });

  it('stops at the deepest folder still there', async () => {
    const { store } = storeOver(tree);
    await store.load();

    await store.goTo({ ...PROJECTS_AT, path: 'projects/gone/deeper' });
    expect(store.place).toEqual(PROJECTS_AT);

    await store.goTo({ ...PROJECTS_AT, node: 'hq/gone' });
    expect(store.place).toBeUndefined();
    expect(store.rows).toHaveLength(2);
  });

  it('reads a listing once, because a listing named by its hash never changes', async () => {
    const { stub, store } = storeOver(tree);
    await store.load();

    await store.goTo(PROJECTS_AT);
    await store.goTo(undefined);
    await store.goTo(PROJECTS_AT);

    expect(stub.count(`GET ${MOUNT}/files/stores/hq/local/${PROJECTS}`)).toBe(
      1,
    );
  });

  it('keeps the other stores when the top of one cannot be read', async () => {
    const { store } = storeOver({
      ...tree,
      [`GET ${MOUNT}/files/stores/hq/local/${ROOT_HQ}`]: problem(
        502,
        'com.prusa.fractal/CALL_FAILED',
      ),
    });

    await store.load();

    expect(store.error).toBeUndefined();
    expect(store.unread.map((one) => one.node)).toEqual(['hq']);
    expect(store.rows.map((row) => row.path)).toEqual(['jigs']);
  });

  it('keeps a refused list of stores, and says it has answered', async () => {
    const { store } = storeOver({
      [STORES]: problem(403, 'core.authz/FORBIDDEN'),
    });

    await store.load();

    expect(store.loaded).toBe(true);
    expect(store.error).toBeDefined();
    expect(store.stores).toEqual([]);
  });

  it('keeps the open folder when the next one cannot be read', async () => {
    const { store } = storeOver({
      ...tree,
      [`GET ${MOUNT}/files/stores/hq/local/${PROJECTS}`]: problem(
        502,
        'com.prusa.fractal/CALL_FAILED',
      ),
    });
    await store.load();

    await store.goTo(PROJECTS_AT);

    expect(store.error).toBeDefined();
    expect(store.trail).toEqual([]);
    expect(store.rows).toHaveLength(2);
  });

  it('forgets everything on a reset', async () => {
    const { store } = storeOver(tree);
    await store.load();

    store.reset();

    expect(store.loaded).toBe(false);
    expect(store.node).toBe('');
    expect(store.rows).toEqual([]);
  });
});

describe('FractalStores while it reads', () => {
  // A listing of PROJECTS that answers only when the test says so.
  const held = () => {
    let answer: (response: Response) => void = () => undefined;
    const routes: Routes = {
      ...tree,
      [`GET ${MOUNT}/files/stores/hq/local/${PROJECTS}`]: () =>
        new Promise<Response>((resolve) => {
          answer = resolve;
        }),
    };

    return {
      routes,
      answer: (response: Response) => {
        answer(response);
      },
    };
  };

  it('says it is loading while it reads, and not after', async () => {
    const { store } = storeOver(tree);

    const read = store.load();
    expect(store.loading).toBe(true);
    await read;

    expect(store.loading).toBe(false);
  });

  it('shows the top, not loading, when the reader goes back before a folder answers', async () => {
    const { routes, answer } = held();
    const { store } = storeOver(routes);
    await store.load();

    const opening = store.goTo(PROJECTS_AT);
    expect(store.loading).toBe(true);
    await store.goTo(undefined);

    expect(store.loading).toBe(false);
    expect(store.rows).toHaveLength(2);

    // The folder's answer lands after the reader left it, and changes nothing.
    answer(
      json(200, {
        node: 'hq',
        instance: 'local',
        identity: PROJECTS,
        entries: [],
      }),
    );
    await opening;
    expect(store.trail).toEqual([]);
    expect(store.rows).toHaveLength(2);
  });

  it('replaces the rows once per answer, and a loading flip leaves them alone', async () => {
    const { store } = storeOver(tree);
    let drawn = 0;
    let seen: unknown;
    const cleanup = $effect.root(() => {
      $effect(() => {
        seen = store.rows;
        drawn += 1;
      });
    });
    flushSync();
    expect(drawn).toBe(1);

    await store.load();
    flushSync();
    expect(drawn).toBe(2);

    await store.goTo(PROJECTS_AT);
    flushSync();
    expect(drawn).toBe(3);

    expect(seen).toBe(store.rows);
    cleanup();
  });
});
