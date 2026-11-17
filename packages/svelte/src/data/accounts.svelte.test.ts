import { describe, expect, it } from 'vitest';

import { createAccounts } from './accounts.svelte';
import { accountIdOf } from './services/accounts';
import type {
  Account,
  AccountRoleName,
  AccountsClient,
} from './services/accounts';
import { ACCOUNT_ROLES } from './services/accounts';
import { StubApiError } from './stubs';

const person = (
  username: string,
  role: AccountRoleName,
  grants: readonly string[] = [],
): Account => ({
  created_at: '2026-09-12T00:00:00Z',
  display_name: username,
  grants: [...grants],
  id: `id-${username}`,
  role,
  subject: `user:${username}`,
  username,
});

interface Calls {
  list: number;
  created: string[];
  deleted: string[];
  passwords: string[];
  /** The `current_password` each password call carried, absent when none. */
  currents: (string | undefined)[];
  roles: string[];
}

const stubClient = (
  rows: readonly Account[],
  options: {
    readonly refuse?: StubApiError;
  } = {},
): { client: AccountsClient; calls: Calls } => {
  const calls: Calls = {
    list: 0,
    created: [],
    deleted: [],
    passwords: [],
    currents: [],
    roles: [],
  };
  const held = [...rows];

  const roleOperation: AccountsClient['setAccountRole'] = (request) => {
    calls.roles.push(`${request.path.account}:${request.body.role}`);
    const at = held.findIndex((row) => row.username === request.path.account);
    const found = held[at];
    if (found !== undefined) held[at] = { ...found, role: request.body.role };

    return Promise.resolve();
  };

  const client: AccountsClient = {
    listAccounts: () => {
      calls.list += 1;

      return Promise.resolve({ items: [...held] });
    },
    createAccount: (request) => {
      if (options.refuse) throw options.refuse;
      const { username, role } = request.body;
      calls.created.push(username);
      const made = person(username, role ?? 'viewer');
      held.push(made);

      return Promise.resolve(made);
    },
    deleteAccount: (request) => {
      calls.deleted.push(request.path.account);
      const at = held.findIndex((row) => row.username === request.path.account);
      if (at >= 0) held.splice(at, 1);

      return Promise.resolve();
    },
    setAccountPassword: (request) => {
      if (options.refuse) throw options.refuse;
      calls.passwords.push(request.path.account);
      calls.currents.push(request.body.current_password);

      return Promise.resolve();
    },
    setAccountRole: roleOperation,
  };

  return { client, calls };
};

describe('Accounts', () => {
  it('starts unread, which is not the same as a server with nobody on it', () => {
    const { client } = stubClient([]);
    const store = createAccounts(client);

    expect(store.items).toEqual([]);
    expect(store.loaded).toBe(false);
    expect(store.error).toBeUndefined();
  });

  it('reads the accounts with their roles and grants', async () => {
    const { client } = stubClient([
      person('alice', 'admin', ['printer-1']),
      person('bob', 'viewer'),
    ]);
    const store = createAccounts(client);

    await store.load();

    expect(store.loaded).toBe(true);
    expect(store.byRole).toEqual({ admin: 1, viewer: 1 });
    expect(store.grants('id-alice')).toEqual(['printer-1']);
  });

  // svelte-data.md: two readers of the same list cost one request.
  it('costs one request however many readers ask at once', async () => {
    const { client, calls } = stubClient([person('alice', 'admin')]);
    const store = createAccounts(client);

    await Promise.all([store.load(), store.load()]);

    expect(calls.list).toBe(1);
  });

  it('offers the three roles, least power first', () => {
    const { client } = stubClient([]);

    expect(createAccounts(client).roles).toEqual(ACCOUNT_ROLES);
    expect(ACCOUNT_ROLES).toEqual(['viewer', 'operator', 'admin']);
  });

  it('adds a person with a role and reads the list again', async () => {
    const { client, calls } = stubClient([]);
    const store = createAccounts(client);

    const made = await store.create({
      username: 'carol',
      password: 'a-long-password',
      role: 'operator',
    });

    expect(made?.role).toBe('operator');
    expect(calls.created).toEqual(['carol']);
    expect(store.items.map((row) => row.username)).toEqual(['carol']);
  });

  it('refuses a person nobody named, without a request', async () => {
    const { client, calls } = stubClient([]);
    const store = createAccounts(client);

    const made = await store.create({ username: '  ', password: 'secret' });

    expect(made).toBeUndefined();
    expect(calls.created).toEqual([]);
  });

  it('reports a refusal by its code and never as a sentence', async () => {
    const { client } = stubClient([], {
      refuse: new StubApiError('core.auth/ACCOUNT_EXISTS', 409),
    });
    const store = createAccounts(client);

    await store.create({ username: 'alice', password: 'secret' });

    expect(store.error?.code).toBe('core.auth/ACCOUNT_EXISTS');
    expect(store.error?.status).toBe(409);
  });

  it('sets a new password and removes a person', async () => {
    const { client, calls } = stubClient([person('bob', 'viewer')]);
    const store = createAccounts(client);
    await store.load();

    await store.changePassword('bob', 'another-long-password');
    await store.remove('bob');

    expect(calls.passwords).toEqual(['bob']);
    expect(calls.deleted).toEqual(['bob']);
    expect(store.items).toEqual([]);
  });

  // The core refuses your own account without it.
  it('sends the current password when the account is the signed-in one', async () => {
    const { client, calls } = stubClient([person('bob', 'viewer')]);
    const store = createAccounts(client);

    await store.changePassword(
      'id-bob',
      'another-long-password',
      'the-old-one',
    );

    expect(calls.passwords).toEqual(['id-bob']);
    expect(calls.currents).toEqual(['the-old-one']);
  });

  it('sends no current password when an administrator resets another account', async () => {
    const { client, calls } = stubClient([person('bob', 'viewer')]);
    const store = createAccounts(client);

    await store.changePassword('id-bob', 'another-long-password');

    expect(calls.currents).toEqual([undefined]);
  });

  // Somebody changing their own password may hold no grant to list accounts.
  it('does not read the listing again after a password change', async () => {
    const { client, calls } = stubClient([person('bob', 'viewer')]);
    const store = createAccounts(client);
    await store.load();

    await store.changePassword(
      'id-bob',
      'another-long-password',
      'the-old-one',
    );

    expect(calls.list).toBe(1);
    expect(store.error).toBeUndefined();
  });

  it('reports a refused password change by its code', async () => {
    const { client } = stubClient([person('bob', 'viewer')], {
      refuse: new StubApiError('core.auth/PASSWORD_WRONG', 403),
    });
    const store = createAccounts(client);

    await store.changePassword('id-bob', 'another-long-password', 'wrong');

    expect(store.error?.code).toBe('core.auth/PASSWORD_WRONG');
  });

  it('changes a role', async () => {
    const { client, calls } = stubClient([person('bob', 'viewer')]);
    const store = createAccounts(client);
    await store.load();

    await store.changeRole('bob', 'operator');

    expect(calls.roles).toEqual(['bob:operator']);
    expect(store.items[0]?.role).toBe('operator');
    expect(store.error).toBeUndefined();
  });
});

describe('accountIdOf', () => {
  it('reads the account id out of a user subject', () => {
    expect(accountIdOf('user:01HXYZ')).toBe('01HXYZ');
  });

  it('answers nothing for a subject no local account is behind', () => {
    expect(accountIdOf('device:mk4-01')).toBeUndefined();
    expect(accountIdOf('user:')).toBeUndefined();
    expect(accountIdOf(undefined)).toBeUndefined();
  });
});

// A browser can sign out and sign in as somebody else without the page
// reloading. The store then holds nothing the first person could read.
describe('Accounts reset', () => {
  it('forgets every account the last person could read', async () => {
    const { client } = stubClient([person('ada', 'admin')]);
    const store = createAccounts(client);
    await store.load();

    store.reset();

    expect(store.items).toEqual([]);
    expect(store.loaded).toBe(false);
    expect(store.loading).toBe(false);
    expect(store.error).toBeUndefined();
    expect(store.byRole).toEqual({});
  });

  it('drops an answer that was asked for before the reset', async () => {
    let answer: (() => void) | undefined;
    const client: AccountsClient = {
      ...stubClient([]).client,
      listAccounts: () =>
        new Promise((resolve) => {
          answer = () => {
            resolve({ items: [person('ada', 'admin')] });
          };
        }),
    };
    const store = createAccounts(client);
    const read = store.load();

    store.reset();
    answer?.();
    await read;

    expect(store.items).toEqual([]);
    expect(store.loaded).toBe(false);
  });

  it('drops a refusal that was asked for before the reset', async () => {
    let refuse: (() => void) | undefined;
    const client: AccountsClient = {
      ...stubClient([]).client,
      listAccounts: () =>
        new Promise((_resolve, reject) => {
          refuse = () => {
            reject(new StubApiError('core.authz/REFUSED', 403));
          };
        }),
    };
    const store = createAccounts(client);
    const read = store.load();

    store.reset();
    refuse?.();
    await read;

    expect(store.error).toBeUndefined();
  });

  // A change of person resets the store first, so the rows a failed read
  // keeps are the same person's.
  it('keeps the rows of the same person beside a failed read', async () => {
    let refused = false;
    const base = stubClient([person('ada', 'admin')]).client;
    const client: AccountsClient = {
      ...base,
      listAccounts: (...args) =>
        refused
          ? Promise.reject(new StubApiError('core.authz/REFUSED', 403))
          : base.listAccounts(...args),
    };
    const store = createAccounts(client);
    await store.load();

    refused = true;
    await store.load();

    expect(store.error?.code).toBe('core.authz/REFUSED');
    expect(store.items.map((row) => row.username)).toEqual(['ada']);
  });
});
