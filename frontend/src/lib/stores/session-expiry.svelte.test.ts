// The session ending under an open page: the 401 and `auth.expired` ends, the
// address carried across the sign-in, and the check that it stays on this
// origin.
import { PrusaConnect } from '@prusa/connect';
import { Session } from '@prusa/connect-ui/data';
import { describe, expect, it, vi } from 'vitest';

import { json, problem, stubFetch } from '../../test/stub';
import {
  addressAfterExpiry,
  addressAfterSignIn,
  rereadOnRefusal,
  safeNext,
  searchBeforeSignIn,
  signInFailureOf,
  withoutSignInFailure,
} from './session-expiry';

describe('the address a sign-in returns to', () => {
  it.each([
    ['/printers/abc', '/printers/abc'],
    ['/storage?folder=a%2Fb#top', '/storage?folder=a%2Fb#top'],
    ['/', '/'],
  ])('keeps %s', (next, kept) => {
    expect(safeNext(next)).toBe(kept);
  });

  it.each([
    null,
    '',
    'printers',
    '//evil.example/x',
    '///evil.example',
    '/\\evil.example',
    '/\t/evil.example',
    'https://evil.example/x',
    'javascript:alert(1)',
    '\\\\evil.example',
  ])('refuses %j', (next) => {
    expect(safeNext(next)).toBeUndefined();
  });

  it('carries the open address out and back', () => {
    const away = addressAfterExpiry('/', '/printers/abc', '?tab=jobs');
    expect(away).toBe('/?next=%2Fprinters%2Fabc%3Ftab%3Djobs');
    expect(addressAfterSignIn('/', away.slice(1))).toBe(
      '/printers/abc?tab=jobs',
    );
  });

  it('carries nothing from the home page, and nothing back when none was carried', () => {
    expect(addressAfterExpiry('/', '/', '')).toBe('/');
    expect(addressAfterSignIn('/', '')).toBeUndefined();
  });

  // The provider sends the browser back to the home page, so a link opened
  // while signed out, such as an install link from the marketplace, travels
  // as next and opens again once the person is signed in.
  it('carries an open link across a sign-in through a provider', () => {
    const kept = searchBeforeSignIn(
      '/',
      '/settings/extensions',
      '?install=github.com%2Facme%2Fspools&version=1.2.0',
    );
    expect(addressAfterSignIn('/', kept)).toBe(
      '/settings/extensions?install=github.com%2Facme%2Fspools&version=1.2.0',
    );
    // On the home page the query is kept as it is, next included.
    expect(searchBeforeSignIn('/', '/', '?next=%2Fjobs')).toBe('?next=%2Fjobs');
  });

  it('sends a person home when the carried address is not safe', () => {
    expect(addressAfterSignIn('/', '?next=%2F%2Fevil.example')).toBe('/');
  });
});

describe('a 401 from any call', () => {
  const signedIn = async (routes: Parameters<typeof stubFetch>[0]) => {
    const stub = stubFetch(routes);
    let handler: () => void = () => undefined;
    // Passed as an arrow: the handler is read when a 401 arrives, after the
    // session below exists.
    const client = new PrusaConnect({
      auth: { kind: 'session' },
      fetch: stub.fetch,
      onUnauthorized: () => {
        handler();
      },
    });
    const session = new Session(client);
    await session.read();
    handler = rereadOnRefusal(session);

    return { stub, client, session };
  };

  it('asks who this browser is, once, and shows the sign-in with the notice', async () => {
    let principalAnswers = 0;
    const { stub, client, session } = await signedIn({
      'GET /api/v1/principal': () =>
        principalAnswers++ === 0
          ? json(200, { principal: 'user:ada' })
          : problem(401, 'core.auth/UNAUTHENTICATED'),
      'GET /api/v1/devices': problem(401, 'core.auth/UNAUTHENTICATED'),
    });
    expect(session.status).toBe('signed-in');

    await Promise.allSettled([client.listDevices(), client.listDevices()]);
    await vi.waitFor(() => {
      expect(session.status).toBe('signed-out');
    });

    expect(session.ended).toBe('refused');
    expect(
      stub.calls.filter((call) => call.path === '/api/v1/principal'),
    ).toHaveLength(2);
  });

  it('does nothing for a 401 while nobody is signed in', async () => {
    const stub = stubFetch({
      'GET /api/v1/devices': problem(401, 'core.auth/UNAUTHENTICATED'),
    });
    const session = new Session(
      new PrusaConnect({ auth: { kind: 'session' }, fetch: stub.fetch }),
    );
    const handler = rereadOnRefusal(session);
    const client = new PrusaConnect({
      auth: { kind: 'session' },
      fetch: stub.fetch,
      onUnauthorized: handler,
    });

    await client.listDevices().catch(() => undefined);

    expect(stub.count('GET /api/v1/principal')).toBe(0);
  });
});

describe('a sign-in the provider sent back unfinished', () => {
  it('reads the reason the server named, and an unknown one as failed', () => {
    expect(signInFailureOf('?sign_in_error=id_token')).toBe('id_token');
    expect(signInFailureOf('?sign_in_error=provider')).toBe('provider');
    expect(signInFailureOf('?sign_in_error=something-new')).toBe('failed');
    expect(signInFailureOf('?next=%2Fprinters')).toBeUndefined();
    expect(signInFailureOf('')).toBeUndefined();
  });

  it('drops the reason from the address and keeps the rest', () => {
    expect(withoutSignInFailure('/', '?sign_in_error=id_token')).toBe('/');
    expect(
      withoutSignInFailure('/', '?sign_in_error=subject&next=%2Fprinters'),
    ).toBe('/?next=%2Fprinters');
  });
});
