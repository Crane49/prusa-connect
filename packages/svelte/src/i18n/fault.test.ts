import { describe, expect, it } from 'vitest';

import { describeFault } from './fault';

/** An `ApiError` as the SDK throws it: the code, and the code again in the message. */
const apiError = (code: string, message: string): unknown =>
  Object.assign(new Error(`${code}: ${message}`), { code, status: 401 });

describe('describeFault', () => {
  it('translates the code and says nothing twice', () => {
    const said = describeFault(
      apiError('core.auth/LOGIN_REFUSED', 'The username or password is wrong'),
    );

    expect(said.title).toBe('The username or password is wrong');
    expect(said.detail).toBeUndefined();
  });

  it('keeps the detail the registry line cannot carry', () => {
    const said = describeFault(
      apiError('core.auth/LOGIN_REFUSED', 'no account named maker'),
    );

    expect(said.title).toBe('The username or password is wrong');
    expect(said.detail).toBe('no account named maker');
  });

  it('falls back to the server line for a code nothing is written against', () => {
    const said = describeFault(apiError('core.made/UP', 'something gave way'));

    expect(said.title).toBe('something gave way');
    expect(said.detail).toBeUndefined();
  });

  it('names a throw that carries no code at all', () => {
    const said = describeFault(new Error('the connection dropped'));

    expect(said.code).toBeUndefined();
    expect(said.title).toBe('the connection dropped');
  });
});
