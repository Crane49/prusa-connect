import type { Accounts } from '../../data';
import type { ListingStore } from '../../primitives';

/** A column of the listing. A reader puts any of them away. */
export type PeopleFieldId = 'person' | 'name' | 'role' | 'added' | 'actions';

/**
 * One person as the listing draws them: the account flattened onto the facts a
 * table cell and a stacked row both read, plus the two verdicts that decide
 * which controls this row may offer.
 */
export interface PersonRow {
  id: string;
  username: string;
  /**
   * The label the server holds. An empty one takes the username, which is what
   * the core says `display_name` falls back to.
   */
  displayName: string;
  /** `viewer`, `operator`, `admin`, or empty for an account holding no role. */
  role: string;
  /** ISO 8601, as the core answers it. */
  createdAt: string;
  /** The signed-in account, which may not change its own role or remove itself. */
  isSelf: boolean;
  /** The last administrator, whose demotion or removal would lock the server. */
  isLastAdmin: boolean;
}

/**
 * What the screen reads and asks of the people on this server. The `Accounts`
 * store satisfies it, so this product hands its store straight in and a
 * product on a different core hands in an object of its own.
 */
export type PeopleSource = Pick<
  Accounts,
  | 'changePassword'
  | 'changeRole'
  | 'create'
  | 'error'
  | 'items'
  | 'loaded'
  | 'loading'
  | 'remove'
  | 'roles'
  | 'writing'
>;

export interface SettingsPeopleScreenProps {
  people: PeopleSource;
  /**
   * The signed-in subject, `user:alice`, which is what an account's own
   * `subject` is compared against. Absent marks no row as the reader's.
   */
  subject?: string | undefined;
  store?: ListingStore;
  /** Where one person's page is. Absent, a row opens nothing. */
  personHref?: ((id: string) => string) | undefined;
}
