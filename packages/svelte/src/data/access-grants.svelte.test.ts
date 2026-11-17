import { describe, expect, it } from 'vitest';

import { AccessGrants } from './access-grants.svelte';
import {
  type Grant,
  type GrantsClient,
  readDeviceGrants,
} from './services/grants';
import { StubApiError } from './stubs';

const BOB = 'user:bob';

const grant = (relation: string, subject = BOB, device = 'p1'): Grant => ({
  object: `device:${device}`,
  relation,
  subject,
});

interface Calls {
  reads: number;
  writes: string[];
}

/** A server holding `held`, which may refuse withdrawals. */
const stubClient = (
  held: Grant[],
  options: { refuseWithdraw?: boolean } = {},
): { client: GrantsClient; calls: Calls } => {
  const calls: Calls = { reads: 0, writes: [] };
  const client: GrantsClient = {
    listGrants: () => {
      calls.reads += 1;

      return Promise.resolve({ items: [...held] });
    },
    listSubjectDirectGrants: (request) => {
      calls.reads += 1;

      return Promise.resolve({
        subject: request.path.subject,
        items: held.filter((g) => g.subject === request.path.subject),
      });
    },
    writeGrant: (request) => {
      calls.writes.push(`+${request.body.relation}`);
      const made = grant(request.body.relation, request.body.subject);
      held.push(made);

      return Promise.resolve(made);
    },
    withdrawGrant: (request) => {
      calls.writes.push(`-${request.query.relation}`);
      if (options.refuseWithdraw) {
        return Promise.reject(new StubApiError('core.authz/ACCESS_DENIED'));
      }
      const at = held.findIndex(
        (g) =>
          g.relation === request.query.relation &&
          g.subject === request.query.subject,
      );
      if (at >= 0) held.splice(at, 1);

      return Promise.resolve();
    },
  };

  return { client, calls };
};

const deviceStore = (client: GrantsClient): AccessGrants =>
  new AccessGrants(client, (c, signal) => readDeviceGrants(c, 'p1', signal));

describe('AccessGrants', () => {
  it('reads once for two loads that overlap', async () => {
    const { client, calls } = stubClient([grant('viewer')]);
    const store = deviceStore(client);

    await Promise.all([store.load(), store.load()]);

    expect(calls.reads).toBe(1);
    expect(store.direct('p1', BOB)).toEqual(['viewer']);
  });

  it('writes the new role, withdraws the old, and reads again', async () => {
    const { client, calls } = stubClient([grant('operator')]);
    const store = deviceStore(client);
    await store.load();

    await store.setChoice('p1', BOB, 'viewer');

    expect(calls.writes).toEqual(['+viewer', '-operator']);
    expect(calls.reads).toBe(2);
    expect(store.direct('p1', BOB)).toEqual(['viewer']);
    expect(store.busy('p1', BOB)).toBe(false);
  });

  it('draws what the server holds after a sequence refused halfway', async () => {
    const { client } = stubClient([grant('operator')], {
      refuseWithdraw: true,
    });
    const store = deviceStore(client);
    await store.load();

    await store.setChoice('p1', BOB, 'viewer');

    expect(store.error?.code).toBe('core.authz/ACCESS_DENIED');
    expect(store.direct('p1', BOB).sort()).toEqual(['operator', 'viewer']);
  });

  it('blocks with a block grant and keeps the printer role', async () => {
    const { client, calls } = stubClient([grant('operator')]);
    const store = deviceStore(client);
    await store.load();

    await store.setOn('p1', BOB, 'viewer', false);

    expect(calls.writes).toEqual(['+blocked']);
    expect(store.direct('p1', BOB).sort()).toEqual(['blocked', 'operator']);
  });

  it('ignores a second change to a row whose first is in flight', async () => {
    const { client, calls } = stubClient([]);
    const store = deviceStore(client);
    await store.load();

    await Promise.all([
      store.setOn('p1', BOB, '', true),
      store.setOn('p1', BOB, '', true),
    ]);

    expect(calls.writes).toEqual(['+viewer']);
  });

  it('keeps a refused read as a fault', async () => {
    const client: GrantsClient = {
      ...stubClient([]).client,
      listGrants: () =>
        Promise.reject(new StubApiError('core.authz/ACCESS_DENIED')),
    };
    const store = deviceStore(client);

    await store.load();

    expect(store.loaded).toBe(false);
    expect(store.error?.status).toBe(403);
  });
});
