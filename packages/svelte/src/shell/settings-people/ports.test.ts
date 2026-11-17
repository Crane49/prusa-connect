import { describe, expect, it } from 'vitest';

import { Accounts, type Account, type AccountsClient } from '../../data';
import { oneAdmin, people, SUBJECT } from './people.fixtures';
import { personRowsOf, roleLabelKey } from './presentation';
import type { PeopleSource } from './types';

const someone: Account = {
  id: 'newcomer',
  username: 'newcomer@printfarm.example',
  display_name: '',
  role: 'viewer',
  subject: 'user:newcomer',
  created_at: '2026-09-17T09:00:00Z',
  grants: ['viewer'],
};

const fakeAccountsClient = (items: Account[]): AccountsClient => ({
  listAccounts: () => Promise.resolve({ items }),
  createAccount: () => Promise.resolve(someone),
  deleteAccount: () => Promise.resolve(),
  setAccountPassword: () => Promise.resolve(),
  setAccountRole: () => Promise.resolve(),
});

describe('the people port', () => {
  it('is satisfied by the store this product hands in', () => {
    // Structural typing makes the match silent in both directions, so it is
    // asserted rather than assumed.
    const source: PeopleSource = new Accounts(fakeAccountsClient([]));

    expect(source.loaded).toBe(false);
  });
});

describe('the rows the accounts spell out', () => {
  it('marks the reader by subject and nobody else', () => {
    const rows = personRowsOf(people, SUBJECT);

    expect(rows.filter((row) => row.isSelf).map((row) => row.id)).toEqual([
      'operator',
    ]);
  });

  it('marks no row at all when nobody is signed in', () => {
    expect(personRowsOf(people, undefined).some((row) => row.isSelf)).toBe(
      false,
    );
  });

  it('calls no administrator the last one while a second holds the role', () => {
    expect(personRowsOf(people, SUBJECT).some((row) => row.isLastAdmin)).toBe(
      false,
    );
  });

  it('marks the only administrator, whoever is reading', () => {
    const rows = personRowsOf(oneAdmin, SUBJECT);

    expect(rows.filter((row) => row.isLastAdmin).map((row) => row.id)).toEqual([
      'jana',
    ]);
  });

  it('takes the username as the label the core says an empty name falls back to', () => {
    const intern = personRowsOf(people, SUBJECT).find(
      (row) => row.id === 'intern',
    );

    expect(intern?.displayName).toBe('intern@printfarm.example');
  });

  it('gives an account holding no role the key that says so', () => {
    expect(roleLabelKey('')).toBe('settingsPeople.role.none');
    expect(roleLabelKey('admin')).toBe('settingsPeople.role.admin');
  });
});
