// Moved from frontend/src/lib/stores/credentials.svelte.ts, 2026-09-11
import {
  type ApiCredential,
  type CredentialScope,
  type CredentialsClient,
  mintCredential,
  type MintedCredential,
  readCredentials,
  revokeCredential,
} from './services/settings';

/**
 * The fault as it was thrown, and never as a sentence. A screen renders it
 * through `describeFault`, which reads the registry code and keeps the
 * server's English message beside the translation (frontend-i18n.md rule 5).
 * Flattening it to a message here would throw the code away.
 */
const asError = (thrown: unknown): Error =>
  thrown instanceof Error ? thrown : new Error(String(thrown));

/**
 * The API credentials the signed-in person holds. A shared store, so a class
 * (svelte-5.md); the client comes in through the constructor, which is what
 * lets a test hand it a fake.
 *
 * A minted credential carries its token once and this server keeps only a
 * hash, so the token is held here until the person dismisses it. It is never
 * written to storage: a token in `localStorage` outlives the screen that
 * warned about it.
 */
export class Credentials {
  readonly #client: CredentialsClient;
  #items = $state.raw<readonly ApiCredential[]>([]);
  #loading = $state(false);
  #loaded = $state(false);
  #error = $state<Error | undefined>(undefined);
  #minting = $state(false);
  #minted = $state.raw<MintedCredential | undefined>(undefined);
  #flight: Promise<void> | undefined;
  // Counts the resets. A request that started before a reset records nothing.
  #resets = 0;

  constructor(client: CredentialsClient) {
    this.#client = client;
  }

  /** Read-only by design: the collection is the server's (svelte-5.md). */
  get items(): readonly ApiCredential[] {
    return this.#items;
  }

  get loading(): boolean {
    return this.#loading;
  }

  /** True once a read has answered, which tells an empty list from an unread one. */
  get loaded(): boolean {
    return this.#loaded;
  }

  get error(): Error | undefined {
    return this.#error;
  }

  /** True while a credential is being created or revoked. */
  get minting(): boolean {
    return this.#minting;
  }

  /** The credential just minted, with its token. Shown once and then dropped. */
  get minted(): MintedCredential | undefined {
    return this.#minted;
  }

  /** A second call while one read is in flight is handed that read. */
  load(): Promise<void> {
    if (this.#flight) return this.#flight;

    const flight = this.#read().finally(() => {
      if (this.#flight === flight) this.#flight = undefined;
    });
    this.#flight = flight;

    return flight;
  }

  /**
   * Mints one under a label. An empty label is refused here rather than at
   * the server: a credential nothing names cannot be told from another in the
   * listing, which is what the listing is for.
   */
  async mint(
    name: string,
    days?: number,
    scope: CredentialScope = 'read',
  ): Promise<void> {
    const label = name.trim();
    if (label === '' || this.#minting) return;

    const resets = this.#resets;
    this.#minting = true;
    this.#error = undefined;
    try {
      const minted = await mintCredential(this.#client, label, days, scope);
      if (resets !== this.#resets) return;
      this.#minted = minted;
      await this.#read();
    } catch (thrown) {
      if (resets === this.#resets) this.#error = asError(thrown);
    } finally {
      if (resets === this.#resets) this.#minting = false;
    }
  }

  /**
   * Revokes one, then reads the list again. Whatever was holding that token
   * stops being able to reach this server.
   */
  async revoke(credential: string): Promise<void> {
    if (credential === '' || this.#minting) return;

    const resets = this.#resets;
    this.#minting = true;
    this.#error = undefined;
    try {
      await revokeCredential(this.#client, credential);
      if (resets !== this.#resets) return;
      // The token on screen may be the one just revoked, and a token that no
      // longer works is not something to keep offering.
      this.#minted = undefined;
      await this.#read();
    } catch (thrown) {
      if (resets === this.#resets) this.#error = asError(thrown);
    } finally {
      if (resets === this.#resets) this.#minting = false;
    }
  }

  /** Drops the shown token. The credential itself stays; only the copy goes. */
  dismiss(): void {
    this.#minted = undefined;
  }

  /**
   * Forgets everything this store read, the shown token included. The app
   * calls it when the person signed in changes, so the next person never sees
   * the credentials of the last one. An answer to a request made before the
   * reset is dropped when it arrives.
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
    this.#minting = false;
    this.#minted = undefined;
  }

  async #read(): Promise<void> {
    const resets = this.#resets;
    this.#loading = true;
    try {
      const page = await readCredentials(this.#client);
      if (resets !== this.#resets) return;
      this.#items = page.items ?? [];
      this.#loaded = true;
      this.#error = undefined;
    } catch (thrown) {
      if (resets === this.#resets) this.#error = asError(thrown);
    } finally {
      if (resets === this.#resets) this.#loading = false;
    }
  }
}
