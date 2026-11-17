import { describe, expect, it } from 'vitest';

import { Credentials, type CredentialsClient } from '../../data';
import type { CredentialsSource } from './types';

const client: CredentialsClient = {
  listCredentials: () => Promise.resolve({ items: [] }),
  createCredential: () => Promise.reject(new Error('not asked for here')),
  revokeCredential: () => Promise.reject(new Error('not asked for here')),
};

describe('the credentials port', () => {
  it('is satisfied by the store this product hands in', () => {
    // Structural typing makes the match silent in both directions, so it is
    // asserted rather than assumed.
    const source: CredentialsSource = new Credentials(client);

    expect(source.loaded).toBe(false);
    expect(source.minted).toBeUndefined();
  });
});
