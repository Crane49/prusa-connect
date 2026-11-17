// The dashboards store against a core whose answers the test releases one by
// one, so the order two requests answer in is the test's to choose.
import { describe, expect, it } from 'vitest';

import { Dashboards, type DashboardsStoreClient } from './dashboards.svelte';
import type { Dashboard } from './services/dashboards';
import { StubApiError } from './stubs';

const dashboard = (id: number, revision: number, name = 'Wall'): Dashboard => ({
  id,
  name,
  visibility: 'everyone',
  is_default: false,
  columns: 12,
  density: 'comfortable',
  cards: [],
  revision,
  created: '2026-10-08T09:00:00Z',
  created_by: 'user:demo',
  changed: '2026-10-08T09:00:00Z',
  changed_by: 'user:demo',
});

/** One read the core has not answered yet. */
interface Pending {
  readonly id: number;
  readonly signal: AbortSignal | undefined;
  answer(value: Dashboard): void;
  refuse(thrown: unknown): void;
}

/**
 * A core that holds every `get-dashboard` until the test answers it, and
 * answers every write at once. The cast names the SDK type, as the other
 * fakes in this library do.
 */
const heldCore = () => {
  const reads: Pending[] = [];
  let saved = 1;
  const client = {
    checkAccess: () => Promise.resolve({ allowed: true }),
    listDashboards: () =>
      Promise.resolve({ items: [], total: 0, default_id: null }),
    getDashboard: (
      { path }: { path: { dashboard: string } },
      signal?: AbortSignal,
    ) =>
      new Promise<Dashboard>((resolve, reject) => {
        reads.push({
          id: Number(path.dashboard),
          signal,
          answer: resolve,
          refuse: reject,
        });
      }),
    createDashboard: () => Promise.resolve(dashboard(9, 1, 'Made')),
    replaceDashboard: ({ path }: { path: { dashboard: string } }) => {
      saved += 1;

      return Promise.resolve(dashboard(Number(path.dashboard), saved));
    },
    deleteDashboard: () => Promise.resolve(),
    setDefaultDashboard: ({ body }: { body: { id: number | null } }) =>
      Promise.resolve({ default_id: body.id }),
  };

  return { reads, client: client as unknown as DashboardsStoreClient };
};

const draft = {
  name: 'Wall',
  visibility: 'everyone' as const,
  columns: 12,
  density: 'comfortable' as const,
  cards: [],
};

/** Lets every answered promise run its continuation. */
const settle = () => new Promise<void>((resolve) => setTimeout(resolve, 0));

/** Opens dashboard 4 at revision 1. */
const opened = async () => {
  const { reads, client } = heldCore();
  const store = new Dashboards(client);
  const opening = store.open(4);
  reads.shift()?.answer(dashboard(4, 1));
  await opening;

  return { reads, store };
};

describe('Dashboards poll', () => {
  it('starts with nothing open and nothing loading', () => {
    const store = new Dashboards(heldCore().client);

    expect(store.dashboard).toBeUndefined();
    expect(store.dashboardId).toBeUndefined();
    expect(store.fault).toBeUndefined();
    expect(store.busy).toBe(false);
  });

  it('replaces the dashboard only with a higher revision', async () => {
    const { reads, store } = await opened();
    const shown = store.dashboard;

    const same = store.poll();
    reads.shift()?.answer(dashboard(4, 1, 'Same'));
    await same;
    expect(store.dashboard).toBe(shown);

    const newer = store.poll();
    reads.shift()?.answer(dashboard(4, 2, 'Newer'));
    await newer;
    expect(store.dashboard?.name).toBe('Newer');
  });

  it('keeps a saved dashboard when a poll from before the save answers late', async () => {
    const { reads, store } = await opened();

    const polling = store.poll();
    const late = reads.shift();
    expect(await store.save(4, { ...draft, revision: 1 })).toBe(true);
    expect(store.dashboard?.revision).toBe(2);
    expect(late?.signal?.aborted).toBe(true);

    late?.answer(dashboard(4, 1, 'Before the save'));
    await polling;
    expect(store.dashboard?.revision).toBe(2);
  });

  it('drops a poll when another dashboard is opened', async () => {
    const { reads, store } = await opened();

    const polling = store.poll();
    const late = reads.shift();
    const opening = store.open(7);
    expect(late?.signal?.aborted).toBe(true);

    late?.answer(dashboard(4, 5, 'Old one'));
    await polling;
    reads.shift()?.answer(dashboard(7, 1, 'Office'));
    await opening;
    expect(store.dashboard?.name).toBe('Office');
  });

  it('drops a poll when the dashboard is closed', async () => {
    const { reads, store } = await opened();

    const polling = store.poll();
    const late = reads.shift();
    store.close();
    late?.answer(dashboard(4, 5));
    await polling;

    expect(late?.signal?.aborted).toBe(true);
    expect(store.dashboard).toBeUndefined();
  });

  it('takes a dashboard the core no longer has off the screen', async () => {
    const { reads, store } = await opened();

    const polling = store.poll();
    reads
      .shift()
      ?.refuse(new StubApiError('core.resource/DASHBOARD_NOT_FOUND', 404));
    await polling;

    expect(store.dashboard).toBeUndefined();
    expect(store.dashboardId).toBe(4);
    expect(store.fault).toBeInstanceOf(StubApiError);
  });

  it('keeps what is shown when a poll fails for any other reason', async () => {
    const { reads, store } = await opened();
    const shown = store.dashboard;

    const polling = store.poll();
    reads.shift()?.refuse(new StubApiError('core.server/UNAVAILABLE', 503));
    await polling;

    expect(store.dashboard).toBe(shown);
    expect(store.fault).toBeUndefined();
  });

  it('keeps one poll at a time', async () => {
    const { reads, store } = await opened();

    void store.poll();
    const first = reads.shift();
    void store.poll();

    expect(first?.signal?.aborted).toBe(true);
    expect(reads[0]?.signal?.aborted).toBe(false);
  });
});

describe('Dashboards create', () => {
  it('drops a dashboard still being read, so its answer does not land under the new id', async () => {
    const { reads, client } = heldCore();
    const store = new Dashboards(client);

    const opening = store.open(4);
    const late = reads.shift();
    const made = await store.create(draft);
    expect(made?.id).toBe(9);
    expect(late?.signal?.aborted).toBe(true);

    late?.answer(dashboard(4, 3, 'Old one'));
    await opening;
    await settle();

    expect(store.dashboardId).toBe(9);
    expect(store.dashboard?.name).toBe('Made');
    expect(store.fault).toBeUndefined();
  });
});
