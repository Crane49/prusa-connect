// The people who can sign in to this server.
import {
  type Account,
  ACCOUNT_ROLES,
  type AccountRoleName,
  type AccountsClient,
  createAccount,
  deleteAccount,
  type NewAccount,
  readAccounts,
  setAccountPassword,
  setAccountRole,
} from './services/accounts';
import { type DataFault, faultOf } from './fault';

/** A shared store, so a class (svelte-5.md). */
export class Accounts {
  readonly #client: AccountsClient;

  #items = $state.raw<readonly Account[]>([]);
  #loading = $state(false);
  #loaded = $state(false);
  #error = $state<DataFault | undefined>(undefined);
  #writing = $state(false);
  #flight: Promise<void> | undefined;
  // Counts the resets. A request that started before a reset records nothing.
  #resets = 0;

  constructor(client: AccountsClient) {
    this.#client = client;
  }

  /** Read-only by design: the collection is the server's (svelte-5.md). */
  get items(): readonly Account[] {
    return this.#items;
  }

  get loading(): boolean {
    return this.#loading;
  }

  /** True once a read has answered, which tells an empty server from an unread one. */
  get loaded(): boolean {
    return this.#loaded;
  }

  get error(): DataFault | undefined {
    return this.#error;
  }

  /** True while a create, a delete or a role change is in flight. */
  get writing(): boolean {
    return this.#writing;
  }

  /** The three roles a person is given, least power first. */
  readonly roles: readonly AccountRoleName[] = ACCOUNT_ROLES;

  /** How many people this server has, by role. */
  readonly byRole: Readonly<Record<string, number>> = $derived.by(() => {
    const counts: Record<string, number> = {};
    for (const row of this.#items) {
      const role = row.role ?? '';
      counts[role] = (counts[role] ?? 0) + 1;
    }

    return counts;
  });

  /** The grants one account was given, or an empty list for an unknown one. */
  grants(id: string): readonly string[] {
    return this.#items.find((row) => row.id === id)?.grants ?? [];
  }

  /** A second call while one read is in flight is given that read. */
  load(): Promise<void> {
    if (this.#flight) return this.#flight;

    const flight = this.#read().finally(() => {
      if (this.#flight === flight) this.#flight = undefined;
    });
    this.#flight = flight;

    return flight;
  }

  /** Adds one person with a role. An empty username is refused here. */
  async create(account: NewAccount): Promise<Account | undefined> {
    if (account.username.trim() === '') return undefined;

    return this.#write(() => createAccount(this.#client, account));
  }

  /** Moves one person to another role. */
  async changeRole(account: string, role: AccountRoleName): Promise<void> {
    await this.#write(async () => {
      await setAccountRole(this.#client, account, role);

      return undefined;
    });
  }

  /** Sets a new password on one account. */
  async changePassword(
    account: string,
    password: string,
    current?: string,
  ): Promise<void> {
    if (password === '') return;

    await this.#attempt(async () => {
      await setAccountPassword(this.#client, account, password, current);
    });
  }

  async remove(account: string): Promise<void> {
    await this.#write(async () => {
      await deleteAccount(this.#client, account);

      return undefined;
    });
  }

  /** A write, and the listing read again to show what it did. */
  async #write<T>(run: () => Promise<T>): Promise<T | undefined> {
    return this.#attempt(async () => {
      const resets = this.#resets;
      const answered = await run();
      if (resets === this.#resets) await this.#read();

      return answered;
    });
  }

  /** The in-flight flag and the fault, around one request and no read. */
  async #attempt<T>(run: () => Promise<T>): Promise<T | undefined> {
    if (this.#writing) return undefined;

    const resets = this.#resets;
    this.#writing = true;
    this.#error = undefined;
    try {
      return await run();
    } catch (thrown) {
      if (resets === this.#resets) this.#error = faultOf(thrown);

      return undefined;
    } finally {
      if (resets === this.#resets) this.#writing = false;
    }
  }

  /**
   * Forgets every account this store read. The app calls it when the person
   * signed in changes, so the next person never sees the list the last one
   * was allowed to read. An answer to a request made before the reset is
   * dropped when it arrives.
   *
   * A failed read keeps the rows of an earlier read. Those rows were read for
   * the same person, because a change of person resets the store first.
   */
  reset(): void {
    this.#resets += 1;
    this.#flight = undefined;
    this.#items = [];
    this.#loading = false;
    this.#loaded = false;
    this.#error = undefined;
    this.#writing = false;
  }

  async #read(): Promise<void> {
    const resets = this.#resets;
    this.#loading = true;
    try {
      const page = await readAccounts(this.#client);
      if (resets !== this.#resets) return;
      this.#items = page.items ?? [];
      this.#loaded = true;
      this.#error = undefined;
    } catch (thrown) {
      if (resets === this.#resets) this.#error = faultOf(thrown);
    } finally {
      if (resets === this.#resets) this.#loading = false;
    }
  }
}

/** One store per pane, built where the application builds its `PrusaConnect`. */
export const createAccounts = (client: AccountsClient): Accounts =>
  new Accounts(client);
