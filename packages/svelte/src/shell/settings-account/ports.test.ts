import { describe, expect, it } from 'vitest';

import { Session } from '../../data';
import type { AuthClient } from '../../data/services/auth';
import type { AccountSource } from './types';

const refuse = (): never => {
  throw new Error('this fake answers nothing');
};

const client: AuthClient = {
  getPrincipal: refuse,
  listSubjectGrants: refuse,
  passwordLogin: refuse,
  logout: refuse,
};

describe('the account port', () => {
  it('is satisfied by the store this product hands in', () => {
    // Structural typing makes the match silent in both directions, so it is
    // asserted rather than assumed.
    const source: AccountSource = new Session(client);

    expect(source.status).toBe('unknown');
    expect(source.principal).toBeUndefined();
  });
});
