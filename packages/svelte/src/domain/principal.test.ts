import { describe, expect, it } from 'vitest';

import { type PrincipalClient, readPrincipal } from './principal';

const clientAnswering = (who: {
  principal: string;
  display_name?: string;
  username?: string;
  email?: string;
}): PrincipalClient =>
  ({
    getPrincipal: () => Promise.resolve(who),
    listSubjectGrants: () => Promise.resolve({ devices: [] }),
  }) as unknown as PrincipalClient;

describe('readPrincipal', () => {
  it('names the person by the display name the account carries', async () => {
    const read = await readPrincipal(
      clientAnswering({
        principal: 'user:operator',
        display_name: 'Demo',
        username: 'demo',
      }),
    );

    expect(read.name).toBe('Demo');
  });

  it('falls back to the username when the display name is empty', async () => {
    const read = await readPrincipal(
      clientAnswering({
        principal: 'user:operator',
        display_name: '',
        username: 'demo',
      }),
    );

    expect(read.name).toBe('demo');
  });

  it('leaves a subject with no account unnamed', async () => {
    const read = await readPrincipal(
      clientAnswering({ principal: 'service_account:ci' }),
    );

    expect(read.name).toBeUndefined();
  });

  it('names a person the provider signed in, with the address it sent', async () => {
    const read = await readPrincipal(
      clientAnswering({
        principal: 'user:oidc-wyj7c7bxbvgxunmhhus7ehtwaoxuz7r3',
        display_name: 'Ada Admin',
        email: 'ada@example.com',
      }),
    );

    expect(read.name).toBe('Ada Admin');
    expect(read.email).toBe('ada@example.com');
  });
});
