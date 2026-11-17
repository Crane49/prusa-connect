// What the modules store forgets when the person signed in changes. The
// listing is read through the person's cookie, so it names the pages that
// person may see.
import { describe, expect, it, vi } from 'vitest';

import { fakeLoaderClient } from '../test/fakes';
import { Modules } from './modules.svelte';
import type { ModulesClient } from './services/settings';

const withPage = (namespace: string) => ({
  namespace,
  ui: {
    entry_url: `/api/v1/modules/${namespace}/ui/index.js`,
    library: '^1',
    contributes: {
      pages: [{ path: 'queue', title: 'Queue', hidden: false }],
    },
  },
});

describe('Modules pages', () => {
  it('marks a page the operator hid as hidden, and keeps it', async () => {
    const store = new Modules(
      fakeLoaderClient([
        { ...withPage('queue'), hidden_contributions: ['pages/queue'] },
        withPage('other'),
      ]),
    );
    await store.load();

    expect(store.pages.map((page) => [page.namespace, page.hidden])).toEqual([
      ['queue', true],
      ['other', false],
    ]);
  });

  it('marks a page the running module hides as hidden (ADR-20261021-6h2)', async () => {
    const hiding = withPage('queue');
    const store = new Modules(
      fakeLoaderClient([
        { ...hiding, ui: { ...hiding.ui, hidden: ['pages/queue'] } },
        withPage('other'),
      ]),
    );
    await store.load();

    expect(store.pages.map((page) => [page.namespace, page.hidden])).toEqual([
      ['queue', true],
      ['other', false],
    ]);
  });
});

describe('Modules reset', () => {
  it('forgets the pages the last person could see', async () => {
    const store = new Modules(fakeLoaderClient([withPage('queue')]));
    await store.load();
    expect(store.pages.map((page) => page.namespace)).toEqual(['queue']);

    store.reset();

    expect(store.items).toEqual([]);
    expect(store.pages).toEqual([]);
    expect(store.loaded).toBe(false);
    expect(store.loading).toBe(false);
    expect(store.error).toBeUndefined();
  });

  it('drops an answer that was asked for before the reset', async () => {
    const base = fakeLoaderClient([withPage('queue')]);
    let answer: (() => void) | undefined;
    const client: ModulesClient = {
      listResources: (...args) =>
        new Promise((resolve, reject) => {
          answer = () => {
            base.listResources(...args).then(resolve, reject);
          };
        }),
    };
    const store = new Modules(client);
    const read = store.load();

    store.reset();
    answer?.();
    await read;

    expect(store.items).toEqual([]);
    expect(store.loaded).toBe(false);
  });
});

describe('Modules fresh load', () => {
  // Each read answers when the test says, with the items it is handed.
  const deferredClient = () => {
    const reads: {
      signal: AbortSignal | undefined;
      answer: (items: unknown[]) => void;
    }[] = [];
    const listResources = vi.fn<ModulesClient['listResources']>(
      (_request, signal) =>
        new Promise((resolve) => {
          reads.push({
            signal,
            answer: (items) => {
              resolve({ items } as Awaited<
                ReturnType<ModulesClient['listResources']>
              >);
            },
          });
        }),
    );

    return { client: { listResources }, reads, listResources };
  };

  it('lets a fresh load start its own read and lets go of the older answer', async () => {
    const { client, reads, listResources } = deferredClient();
    const store = new Modules(client);

    const older = store.load();
    const fresh = store.load({ fresh: true });
    reads[1]?.answer([withPage('queue')]);
    await fresh;
    reads[0]?.answer([]);
    await older;

    expect(store.items.map((item) => item.namespace)).toEqual(['queue']);
    expect(store.loading).toBe(false);
    expect(reads[0]?.signal?.aborted).toBe(true);
    expect(listResources).toHaveBeenCalledTimes(2);
  });

  it('joins a fresh read already in flight', async () => {
    const { client, reads, listResources } = deferredClient();
    const store = new Modules(client);

    const fresh = store.load({ fresh: true });
    const joined = store.load();
    reads[0]?.answer([]);
    await Promise.all([fresh, joined]);

    expect(listResources).toHaveBeenCalledTimes(1);
  });
});
