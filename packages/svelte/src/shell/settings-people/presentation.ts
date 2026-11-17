// The account the core answers, turned into the row every view of this listing
// reads, plus the two words that depend on the whole list rather than on one
// account.
import type { Account } from '../../data';
import type { ListingStore } from '../../primitives';
import type { PersonRow } from './types';

const ADMIN = 'admin';

/**
 * The key per role, written out so the catalogue gate can see every one of
 * them. An account holding no role can sign in and do nothing, which is a
 * value the core answers and not a missing reading.
 */
export const ROLE_LABEL_KEY: Record<string, string> = {
  admin: 'settingsPeople.role.admin',
  operator: 'settingsPeople.role.operator',
  viewer: 'settingsPeople.role.viewer',
  '': 'settingsPeople.role.none',
};

export const roleLabelKey = (role: string): string =>
  ROLE_LABEL_KEY[role] ?? 'settingsPeople.role.none';

/**
 * Every account as a row. `isLastAdmin` is counted over the whole list, since
 * a row cannot tell on its own whether it is the only way back in.
 */
export const personRowsOf = (
  accounts: readonly Account[],
  subject: string | undefined,
): PersonRow[] => {
  const admins = accounts.filter((account) => account.role === ADMIN).length;

  return accounts.map((account) => ({
    id: account.id,
    username: account.username,
    displayName:
      account.display_name.trim() === ''
        ? account.username
        : account.display_name,
    role: account.role ?? '',
    createdAt: account.created_at,
    isSelf: subject !== undefined && account.subject === subject,
    isLastAdmin: account.role === ADMIN && admins === 1,
  }));
};

/** Whether a search matches any of these texts, ignoring case. */
export const matches = (search: string, ...texts: string[]): boolean => {
  const needle = search.trim().toLocaleLowerCase();

  return (
    needle === '' ||
    texts.some((text) => text.toLocaleLowerCase().includes(needle))
  );
};

/** Where a browser keeps what a reader chose. A test hands in a map instead. */
export const browserStore = (): ListingStore => ({
  read: (key) => localStorage.getItem(key),
  write: (key, value) => {
    localStorage.setItem(key, value);
  },
});
