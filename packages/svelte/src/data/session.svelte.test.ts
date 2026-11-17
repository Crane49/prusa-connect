// What the sign-in screen is told about why it is showing. The store keeps
// three answers apart, and a wrong one puts a false sentence on the first
// screen a person ever sees.
import { flushSync } from 'svelte';
import { describe, expect, it, vi } from 'vitest';

import type { AuthClient } from './services/auth';
import { Session } from './session.svelte';
import { StubApiError } from './stubs';

/**
 * A client that answers the principal while `signedIn` is true and refuses it
 * with a 401 after that, which is the pair of answers the store reads.
 */
const stubAuth = (): { client: AuthClient; allow: (next: boolean) => void } => {
  let signedIn = false;

  const client: AuthClient = {
    getPrincipal: () =>
      signedIn
        ? Promise.resolve({ principal: 'user:demo' })
        : Promise.reject(new StubApiError('core.auth/UNAUTHORIZED', 401)),
    listSubjectGrants: () =>
      Promise.resolve({ devices: [], subject: 'user:demo' }),
    passwordLogin: () => {
      signedIn = true;

      return Promise.resolve({ expires_at: '2026-09-13T00:00:00Z' });
    },
    logout: () => {
      signedIn = false;

      return Promise.resolve();
    },
  };

  return {
    client,
    allow: (next) => {
      signedIn = next;
    },
  };
};

describe('an open server after its first run', () => {
  it('is signed in the moment the open mode is recorded', async () => {
    // An open server serves every request as the configured principal and
    // answers signed_in false, because nobody signed in. The store must read
    // the principal anyway, or the page stays on the sign-in screen with no
    // account to sign in with.
    let recorded = false;
    const client = {
      ...stubAuth().client,
      getPrincipal: () =>
        recorded
          ? Promise.resolve({ principal: 'user:operator' })
          : Promise.reject(new StubApiError('core.auth/SETUP_PENDING', 503)),
      recordSetup: () => {
        recorded = true;

        return Promise.resolve({
          mode: 'open' as const,
          state: 'done' as const,
          signed_in: false,
          steps: [],
        });
      },
    } as unknown as AuthClient;
    const session = new Session(client);
    await session.read();
    flushSync();
    expect(session.status).toBe('signed-out');

    await session.setup({ mode: 'open' });
    flushSync();
    expect(session.status).toBe('signed-in');
    expect(session.principal?.id).toBe('user:operator');
  });
});

describe('why the sign-in is showing', () => {
  it('a browser that never signed in is not told its credential was refused', async () => {
    const { client } = stubAuth();
    const session = new Session(client);

    await session.read();
    flushSync();

    expect(session.status).toBe('signed-out');
    expect(session.ended).toBe('never');
  });

  it('a session this server stops accepting is told it was refused', async () => {
    const { client, allow } = stubAuth();
    const session = new Session(client);

    await session.signIn('demo', 'a demo password');
    flushSync();
    expect(session.status).toBe('signed-in');

    // The cookie stops being accepted, and the next read is the only way the
    // page learns it.
    allow(false);
    await session.read();
    flushSync();

    expect(session.status).toBe('signed-out');
    expect(session.ended).toBe('refused');
  });

  it('a person who signs out is told they signed out', async () => {
    const { client } = stubAuth();
    const session = new Session(client);

    await session.signIn('demo', 'a demo password');
    await session.signOut();
    flushSync();

    expect(session.ended).toBe('asked');
  });
});

describe('changing your own password', () => {
  /** What the password route was called with, so the body can be read back. */
  const withPassword = () => {
    const sent: { account: string; body: unknown }[] = [];
    const { client, allow } = stubAuth();

    return {
      sent,
      allow,
      client: {
        ...client,
        setAccountPassword: (request: {
          path: { account: string };
          body: unknown;
        }) => {
          sent.push({ account: request.path.account, body: request.body });

          return Promise.resolve();
        },
      },
    };
  };

  it('sends the current password beside the new one', async () => {
    const { client, sent, allow } = withPassword();
    const session = new Session(client);
    allow(true);
    await session.read();

    await session.changePassword('old-secret', 'new-secret');

    // The server refuses a change to your own account without the current
    // password, so a store that sent only the new one could never succeed.
    expect(sent).toHaveLength(1);
    expect(sent[0]?.body).toEqual({
      current_password: 'old-secret',
      new_password: 'new-secret',
    });
    expect(session.changed).toBe(true);
    expect(session.changing).toBe(false);
  });

  // The graph keys a person as `user:demo`, and the password route takes the
  // part after the colon.
  it('names the account and not the subject', async () => {
    const { client, sent, allow } = withPassword();
    const session = new Session(client);
    allow(true);
    await session.read();

    await session.changePassword('old-secret', 'new-secret');

    expect(sent[0]?.account).toBe('demo');
  });

  it('keeps the refusal and does not say the password changed', async () => {
    const { client, allow } = stubAuth();
    const session = new Session({
      ...client,
      setAccountPassword: () =>
        Promise.reject(new StubApiError('core.auth/PASSWORD_WRONG', 403)),
    });
    allow(true);
    await session.read();

    await session.changePassword('wrong', 'new-secret');

    expect(session.changed).toBe(false);
    expect(session.passwordFault).toBeDefined();
  });

  it('changes nothing for a browser that is signed out', async () => {
    const { client, sent } = withPassword();
    const session = new Session(client);

    await session.changePassword('old-secret', 'new-secret');

    expect(sent).toEqual([]);
    expect(session.changed).toBe(false);
  });
});

describe('the way in a server offers', () => {
  const withInfo = (info: () => Promise<unknown>): Session =>
    new Session({ ...stubAuth().client, getAuthInfo: info } as AuthClient);

  it('names the provider when the server redirects to one', async () => {
    const session = withInfo(() =>
      Promise.resolve({ provider: 'oidc', redirect: true }),
    );
    await session.read();

    expect(session.status).toBe('signed-out');
    expect(session.redirectProvider).toBe('oidc');
  });

  it('names the provider by the name the server gives it', async () => {
    const session = withInfo(() =>
      Promise.resolve({
        provider: 'oidc',
        provider_name: 'sso.example.com',
        redirect: true,
      }),
    );
    await session.read();

    expect(session.redirectProvider).toBe('sso.example.com');
  });

  it('names the provider on a page that opened signed in', async () => {
    const { client, allow } = stubAuth();
    const session = new Session({
      ...client,
      getAuthInfo: () => Promise.resolve({ provider: 'oidc', redirect: true }),
    } as AuthClient);
    allow(true);
    await session.read();
    await vi.waitFor(() => {
      expect(session.redirectProvider).toBe('oidc');
    });

    expect(session.status).toBe('signed-in');
  });

  it('names the provider after a sign-out on a page that opened signed in', async () => {
    const { client, allow } = stubAuth();
    const session = new Session({
      ...client,
      getAuthInfo: () => Promise.resolve({ provider: 'oidc', redirect: true }),
    } as AuthClient);
    allow(true);
    await session.read();
    expect(session.status).toBe('signed-in');

    await session.signOut();
    flushSync();

    expect(session.status).toBe('signed-out');
    expect(session.redirectProvider).toBe('oidc');
  });

  it('keeps the password form for a server that takes passwords', async () => {
    const session = withInfo(() =>
      Promise.resolve({ provider: 'local', redirect: false }),
    );
    await session.read();

    expect(session.redirectProvider).toBeUndefined();
  });

  it('names the provider once the first run restarts the server on one', async () => {
    // The page draws the wizard while the server still takes passwords, and
    // the wizard's last step reads the setup and the principal again on the
    // server that restarted on a provider.
    let restarted = false;
    const session = new Session({
      ...stubAuth().client,
      getAuthInfo: () =>
        Promise.resolve(
          restarted
            ? { provider: 'oidc', redirect: true }
            : { provider: 'local', redirect: false },
        ),
      getSetup: () =>
        Promise.resolve({ state: restarted ? 'done' : 'pending' }),
    } as unknown as AuthClient);
    await session.readSetup();
    await session.read();
    expect(session.redirectProvider).toBeUndefined();

    restarted = true;
    await session.readSetup();
    await session.read();
    flushSync();

    expect(session.status).toBe('signed-out');
    expect(session.redirectProvider).toBe('oidc');
  });

  it('keeps the password form when the server cannot say', async () => {
    const session = withInfo(() => Promise.reject(new Error('down')));
    await session.read();

    expect(session.redirectProvider).toBeUndefined();
  });
});
