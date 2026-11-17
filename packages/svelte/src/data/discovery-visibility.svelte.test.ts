import { describe, expect, it } from 'vitest';

import { DiscoveryVisibility } from './discovery-visibility.svelte';
import type { DiscoveryClient, Visibility } from './services/discovery';
import { StubApiError } from './stubs';

const refuse = (): never => {
  throw new Error('this fake lists nobody');
};

interface Held {
  answer: Visibility;
  written: boolean[];
  reads: number;
  refusal: StubApiError | undefined;
}

const stubClient = (
  start: Visibility,
): { client: DiscoveryClient; held: Held } => {
  const held: Held = {
    answer: start,
    written: [],
    reads: 0,
    refusal: undefined,
  };
  const client: DiscoveryClient = {
    listNearbyPrinters: refuse,
    getDiscoveryVisibility: () => {
      held.reads += 1;

      return Promise.resolve(held.answer);
    },
    setDiscoveryVisibility: (request) => {
      held.written.push(request.body.hidden);
      if (held.refusal) return Promise.reject(held.refusal);

      held.answer = {
        hidden: request.body.hidden,
        announcement: request.body.hidden ? 'hidden' : 'announcing',
        bridged: false,
      };

      return Promise.resolve(held.answer);
    },
  };

  return { client, held };
};

const VISIBLE: Visibility = {
  hidden: false,
  announcement: 'announcing',
  bridged: false,
};

describe('DiscoveryVisibility', () => {
  it('starts unread', () => {
    const store = new DiscoveryVisibility(stubClient(VISIBLE).client);

    expect(store.loaded).toBe(false);
    expect(store.hidden).toBe(false);
    expect(store.announcement).toBeUndefined();
    expect(store.saving).toBe(false);
  });

  it('reads the setting the server applies', async () => {
    const store = new DiscoveryVisibility(
      stubClient({ hidden: true, announcement: 'hidden', bridged: false })
        .client,
    );

    await store.load();

    expect(store.loaded).toBe(true);
    expect(store.hidden).toBe(true);
    expect(store.announcement).toBe('hidden');
  });

  it('keeps what the server says it announces', async () => {
    const store = new DiscoveryVisibility(
      stubClient({
        ...VISIBLE,
        host: 'desk.local',
        port: 8080,
        services: ['_prusalink._tcp'],
      }).client,
    );
    expect(store.described).toBeUndefined();

    await store.load();

    expect(store.described?.host).toBe('desk.local');
    expect(store.described?.port).toBe(8080);
    expect(store.described?.services).toEqual(['_prusalink._tcp']);
  });

  it('hides the server and shows the answer the server gave', async () => {
    const { client, held } = stubClient(VISIBLE);
    const store = new DiscoveryVisibility(client);
    await store.load();

    await store.setHidden(true);

    expect(held.written).toEqual([true]);
    expect(store.hidden).toBe(true);
    expect(store.announcement).toBe('hidden');
    expect(store.saving).toBe(false);
  });

  // A viewer may read the setting and not change it. The refusal is kept by
  // its code, and the switch goes back to what the server holds.
  it('keeps the setting the server holds when a change is refused', async () => {
    const { client, held } = stubClient(VISIBLE);
    const store = new DiscoveryVisibility(client);
    await store.load();

    held.refusal = new StubApiError('core.auth/ACCESS_DENIED', 403);
    await store.setHidden(true);

    expect(store.hidden).toBe(false);
    expect(store.error?.code).toBe('core.auth/ACCESS_DENIED');
    expect(store.saving).toBe(false);
  });

  it('drops a second change while the first is on its way', async () => {
    const { client, held } = stubClient(VISIBLE);
    const store = new DiscoveryVisibility(client);

    const first = store.setHidden(true);
    expect(store.saving).toBe(true);
    await store.setHidden(false);
    await first;

    expect(held.written).toEqual([true]);
    expect(store.hidden).toBe(true);
  });

  // svelte-data.md: two readers of the same setting cost one request.
  it('costs one read however many readers ask at once', async () => {
    const { client, held } = stubClient(VISIBLE);
    const store = new DiscoveryVisibility(client);

    await Promise.all([store.load(), store.load()]);

    expect(held.reads).toBe(1);
  });
});

describe('DiscoveryVisibility reset', () => {
  it('forgets the setting, and an answer to a read sent before the reset records nothing', async () => {
    let answer: (value: Visibility) => void = () => undefined;
    const client: DiscoveryClient = {
      listNearbyPrinters: refuse,
      getDiscoveryVisibility: () =>
        new Promise((resolve) => {
          answer = resolve;
        }),
      setDiscoveryVisibility: refuse,
    };
    const store = new DiscoveryVisibility(client);

    const read = store.load();
    store.reset();
    answer({ hidden: true, announcement: 'hidden', bridged: false });
    await read;

    expect(store.loaded).toBe(false);
    expect(store.hidden).toBe(false);
    expect(store.loading).toBe(false);
  });

  it('forgets a change the last person asked for', async () => {
    let answer: (value: Visibility) => void = () => undefined;
    const client: DiscoveryClient = {
      listNearbyPrinters: refuse,
      getDiscoveryVisibility: refuse,
      setDiscoveryVisibility: () =>
        new Promise((resolve) => {
          answer = resolve;
        }),
    };
    const store = new DiscoveryVisibility(client);

    const change = store.setHidden(true);
    store.reset();
    answer({ hidden: true, announcement: 'hidden', bridged: false });
    await change;

    expect(store.loaded).toBe(false);
    expect(store.saving).toBe(false);
  });

  it('keeps a change when a read sent before it answers after it', async () => {
    let answerRead: (answer: Visibility) => void = () => undefined;
    const client: DiscoveryClient = {
      listNearbyPrinters: refuse,
      getDiscoveryVisibility: () =>
        new Promise((resolve) => {
          answerRead = resolve;
        }),
      setDiscoveryVisibility: (request) =>
        Promise.resolve({
          hidden: request.body.hidden,
          announcement: 'hidden',
          bridged: false,
        }),
    };
    const store = new DiscoveryVisibility(client);

    const read = store.load();
    await store.setHidden(true);
    answerRead(VISIBLE);
    await read;

    expect(store.hidden).toBe(true);
    expect(store.announcement).toBe('hidden');
  });
});
