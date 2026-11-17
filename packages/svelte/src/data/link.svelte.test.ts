import { describe, expect, it } from 'vitest';

import { createLink } from './link.svelte';
import type { LinkClient, LinkInstance, LinkNode } from './services/link';
import { fakeClock, settled, StubApiError } from './stubs';

const nodeOf = (
  id: string,
  admission: 'admitted' | 'pending',
  alive = true,
): LinkNode => ({
  address: `https://${id}.example`,
  admission,
  alive,
  id,
  last_heartbeat: '2026-09-12T00:00:00Z',
  latency_ms: 12,
  public_key: `key-${id}`,
  role: 'child',
});

const linkOf = (
  name: string,
  nodes: readonly LinkNode[] = [],
): LinkInstance => ({
  accepting: true,
  address: 'https://parent.example',
  enabled: true,
  listeners: [],
  enrolment: 'inherit',
  kind: 'link',
  mode: 'parent',
  name,
  node_id: 'node-self',
  nodes: [...nodes],
  public_key: 'key-self',
  running: true,
  type: 'fractal',
});

interface Calls {
  list: number;
  read: string[];
  approved: string[];
}

const stubClient = (
  rows: readonly LinkInstance[],
  refuse?: StubApiError,
): { client: LinkClient; calls: Calls } => {
  const calls: Calls = { list: 0, read: [], approved: [] };
  const held = [...rows];
  const client: LinkClient = {
    listLinks: () => {
      calls.list += 1;

      return Promise.resolve({ items: [...held] });
    },
    getLink: (request) => {
      calls.read.push(request.path.name);
      const found = held.find((row) => row.name === request.path.name);
      if (!found) throw new StubApiError('core.registry/NOT_FOUND', 404);

      return Promise.resolve(found);
    },
    approveLinkNode: (request) => {
      if (refuse) throw refuse;
      calls.approved.push(request.path.id);
      const at = held.findIndex((row) => row.name === request.path.name);
      const found = held[at];
      if (found === undefined) throw new Error('unreachable');
      const admitted: LinkInstance = {
        ...found,
        nodes: (found.nodes ?? []).map((node) =>
          node.id === request.path.id
            ? { ...node, admission: 'admitted' as const }
            : node,
        ),
      };
      held[at] = admitted;

      return Promise.resolve(admitted);
    },
  };

  return { client, calls };
};

describe('Link', () => {
  it('starts unread, which is not the same as a server with no link', () => {
    const { client } = stubClient([]);
    const store = createLink(client);

    expect(store.loaded).toBe(false);
    expect(store.declared).toBe(false);
    expect(store.enabled).toBe(false);
  });

  it('reads the link with its mode, its enrolment and its key', async () => {
    const { client } = stubClient([linkOf('fractal')]);
    const store = createLink(client);

    await store.load();

    expect(store.loaded).toBe(true);
    expect(store.declared).toBe(true);
    expect(store.mode).toBe('parent');
    expect(store.enrolment).toBe('inherit');
    expect(store.publicKey).toBe('key-self');
    expect(store.running).toBe(true);
  });

  // svelte-data.md: two readers of the same list cost one request.
  it('costs one request however many readers ask at once', async () => {
    const { client, calls } = stubClient([linkOf('fractal')]);
    const store = createLink(client);

    await Promise.all([store.load(), store.load()]);

    expect(calls.list).toBe(1);
  });

  it('separates the nodes that are alive from the ones waiting', async () => {
    const { client } = stubClient([
      linkOf('fractal', [
        nodeOf('node-a', 'admitted'),
        nodeOf('node-b', 'pending', false),
      ]),
    ]);
    const store = createLink(client);

    await store.load();

    expect(store.nodes).toHaveLength(2);
    expect(store.alive.map((node) => node.id)).toEqual(['node-a']);
    expect(store.pending.map((node) => node.id)).toEqual(['node-b']);
    expect(store.alive[0]?.latency_ms).toBe(12);
  });

  it('admits a waiting node and replaces the row from the answer', async () => {
    const { client, calls } = stubClient([
      linkOf('fractal', [nodeOf('node-b', 'pending')]),
    ]);
    const store = createLink(client);
    await store.load();

    await store.approve('node-b');

    expect(calls.approved).toEqual(['node-b']);
    expect(calls.list).toBe(1);
    expect(store.pending).toEqual([]);
    expect(store.approving('node-b')).toBe(false);
  });

  it('reports a refusal by its code and never as a sentence', async () => {
    const { client } = stubClient(
      [linkOf('fractal', [nodeOf('node-b', 'pending')])],
      new StubApiError('core.auth/ACCESS_DENIED', 403),
    );
    const store = createLink(client);
    await store.load();

    await store.approve('node-b');

    expect(store.error?.code).toBe('core.auth/ACCESS_DENIED');
    expect(store.error?.status).toBe(403);
  });

  it('looks at one link by name when a server declares two', async () => {
    const { client } = stubClient([linkOf('north'), linkOf('south')]);
    const store = createLink(client);
    await store.load();

    expect(store.current?.name).toBe('north');
    store.select('south');
    expect(store.current?.name).toBe('south');
  });

  it('reads the links again on the tick, and stops on teardown', async () => {
    const clock = fakeClock();
    const { client, calls } = stubClient([linkOf('fractal')]);
    const store = createLink(client, { timers: clock, refreshMs: 5000 });
    const stop = store.listen();

    clock.advance(5000);
    await settled();
    expect(calls.list).toBe(1);

    clock.advance(5000);
    await settled();
    expect(calls.list).toBe(2);

    stop();
    expect(clock.pending).toBe(0);
  });

  it('reads one link again on its own', async () => {
    const { client, calls } = stubClient([linkOf('fractal')]);
    const store = createLink(client);
    await store.load();

    await store.refresh('fractal');

    expect(calls.read).toEqual(['fractal']);
    expect(store.items).toHaveLength(1);
  });
});
