// The sample server this screen's stories and tests read: the account that set
// it up, a second administrator, a shared viewer and an account with no display
// name of its own.
import type { Account, AccountRoleName, DataFault } from '../../data';
import type { PeopleSource } from './types';

/** The signed-in subject, which marks one of the rows below as the reader's. */
export const SUBJECT = 'user:operator';

const account = (
  id: string,
  username: string,
  displayName: string,
  role: string,
  createdAt: string,
): Account => ({
  id,
  username,
  display_name: displayName,
  role,
  subject: `user:${id}`,
  created_at: createdAt,
  grants: role === '' ? [] : [role],
});

export const people: Account[] = [
  account(
    'operator',
    'operator@printfarm.example',
    'Farm operator',
    'admin',
    '2026-01-08T10:22:00Z',
  ),
  account(
    'jana',
    'jana@printfarm.example',
    'Jana Dvořáková',
    'admin',
    '2026-02-14T08:05:00Z',
  ),
  account(
    'workshop',
    'workshop@printfarm.example',
    'Workshop shared account',
    'operator',
    '2026-03-30T16:47:00Z',
  ),
  account(
    'intern',
    'intern@printfarm.example',
    '',
    'viewer',
    '2026-08-18T09:15:00Z',
  ),
];

/** A fresh server: the account that set it up, and nobody else yet. */
export const solePerson: Account[] = people.slice(0, 1);

/** One administrator, and the reader is not it. */
export const oneAdmin: Account[] = people.slice(1);

/** A refusal the fault registry has a line for. */
export const denied: DataFault = {
  code: 'core.auth/ACCESS_DENIED',
  traceId: undefined,
  status: 403,
  params: {},
  message: 'only an administrator may list accounts',
};

const ROLES: readonly AccountRoleName[] = ['viewer', 'operator', 'admin'];

/**
 * A port the tests can watch. Every write answers straight away and changes
 * nothing, so a test asserts on what was asked for rather than on a store.
 */
export const fakePeopleSource = (
  items: readonly Account[],
  overrides: Partial<PeopleSource> = {},
): PeopleSource => ({
  items,
  loading: false,
  loaded: true,
  error: undefined,
  writing: false,
  roles: ROLES,
  create: () => Promise.resolve(undefined),
  changeRole: () => Promise.resolve(),
  changePassword: () => Promise.resolve(),
  remove: () => Promise.resolve(),
  ...overrides,
});
