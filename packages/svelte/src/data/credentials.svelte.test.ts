// What the API credentials room can do: list, create and revoke. The token is
// answered once, so the store holds it until the person is done with it.
import { describe, expect, it } from 'vitest';

import { Credentials } from './credentials.svelte';
import type { ApiCredential, CredentialsClient } from './services/settings';
import { StubApiError } from './stubs';

const credentialOf = (id: string, name: string): ApiCredential => ({
  created_at: '2026-09-01T08:00:00Z',
  expires_at: '2027-09-01T08:00:00Z',
  id,
  name,
  origin: 'mint',
  scopes: ['read'],
  subject: 'user:demo',
});

const stub = (refuse?: StubApiError) => {
  const held: ApiCredential[] = [credentialOf('pk-1', 'laptop')];
  const revoked: string[] = [];
  const asked: (readonly string[] | undefined)[] = [];

  const client: CredentialsClient = {
    listCredentials: () => Promise.resolve({ items: [...held] }),
    createCredential: (request) => {
      asked.push(request.body.scopes);
      const minted = credentialOf('pk-2', request.body.name);
      held.push(minted);

      // A mint answers origin mint.
      return Promise.resolve({
        ...minted,
        origin: 'mint',
        token: 'secret-token',
      });
    },
    revokeCredential: (request) => {
      if (refuse) throw refuse;
      revoked.push(request.path.credential);
      const at = held.findIndex((row) => row.id === request.path.credential);
      if (at >= 0) held.splice(at, 1);

      return Promise.resolve();
    },
  };

  return { client, revoked, asked };
};

describe('Credentials', () => {
  // A credential is never wider than what the person chose, and with no
  // choice it can only read.
  it('mints with the scope the person chose, and read when none', async () => {
    const { client, asked } = stub();
    const store = new Credentials(client);

    await store.mint('display');
    await store.mint('wall', 30, 'operate');

    expect(asked).toEqual([['read'], ['operate']]);
  });

  it('revokes one and reads the list again', async () => {
    const { client, revoked } = stub();
    const store = new Credentials(client);
    await store.load();

    await store.revoke('pk-1');

    expect(revoked).toEqual(['pk-1']);
    expect(store.items).toEqual([]);
  });

  // The token on screen may be the one just revoked, and a token that no
  // longer works is not something to keep offering.
  it('drops the shown token when one is revoked', async () => {
    const { client } = stub();
    const store = new Credentials(client);
    await store.mint('phone');

    expect(store.minted).toBeDefined();
    await store.revoke('pk-2');

    expect(store.minted).toBeUndefined();
  });

  it('keeps the refusal and the credential when a revocation fails', async () => {
    const { client } = stub(new StubApiError('core.authz/REFUSED', 403));
    const store = new Credentials(client);
    await store.load();

    await store.revoke('pk-1');

    expect(store.error).toBeDefined();
    expect(store.items.map((row) => row.id)).toEqual(['pk-1']);
  });

  it('asks nothing for an empty credential', async () => {
    const { client, revoked } = stub();
    const store = new Credentials(client);

    await store.revoke('');

    expect(revoked).toEqual([]);
  });
});

// A browser can sign out and sign in as somebody else without the page
// reloading. The store then holds nothing of the first person.
describe('Credentials reset', () => {
  // A client whose answer is the credentials of whoever is signed in now. A
  // read can be held, so a test decides when its answer arrives.
  const byPerson = () => {
    const state = { who: 'ada', hold: false, refuse: false };
    const held: (() => void)[] = [];
    const client: CredentialsClient = {
      listCredentials: () => {
        if (state.refuse) {
          return Promise.reject(new StubApiError('core.authz/REFUSED', 403));
        }
        const answer = { items: [credentialOf(`pk-${state.who}`, state.who)] };
        if (!state.hold) return Promise.resolve(answer);

        return new Promise((resolve) => {
          held.push(() => {
            resolve(answer);
          });
        });
      },
      createCredential: (request) =>
        Promise.resolve({
          ...credentialOf('pk-new', request.body.name),
          origin: 'mint',
          token: 'secret-token',
        }),
      revokeCredential: () => Promise.resolve(),
    };

    return { client, state, held };
  };

  it('forgets the credentials and the shown token of the last person', async () => {
    const { client } = byPerson();
    const store = new Credentials(client);
    await store.mint('laptop');
    expect(store.items).toHaveLength(1);
    expect(store.minted?.token).toBe('secret-token');

    store.reset();

    expect(store.items).toEqual([]);
    expect(store.loaded).toBe(false);
    expect(store.loading).toBe(false);
    expect(store.error).toBeUndefined();
    expect(store.minted).toBeUndefined();
  });

  it('drops an answer that was asked for before the reset', async () => {
    const { client, state, held } = byPerson();
    const store = new Credentials(client);
    state.hold = true;
    const read = store.load();

    store.reset();
    state.who = 'grace';
    held[0]?.();
    await read;

    expect(store.items).toEqual([]);
    expect(store.loaded).toBe(false);
  });

  it('reads the next person after a reset, and a read in flight is not joined', async () => {
    const { client, state, held } = byPerson();
    const store = new Credentials(client);
    state.hold = true;
    const before = store.load();

    store.reset();
    state.who = 'grace';
    state.hold = false;
    await store.load();
    held[0]?.();
    await before;

    expect(store.items.map((row) => row.name)).toEqual(['grace']);
  });

  // A change of person resets the store first, so the rows a failed read
  // keeps are the same person's.
  it('keeps the rows of the same person beside a failed read', async () => {
    const { client, state } = byPerson();
    const store = new Credentials(client);
    await store.load();

    state.refuse = true;
    await store.load();

    expect(store.error).toBeDefined();
    expect(store.items.map((row) => row.name)).toEqual(['ada']);
  });
});
