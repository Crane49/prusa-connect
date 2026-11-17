// The databases modules left behind, and the purge a person starts from the
// Extensions page (ADR-20261021-bqc), read through the SDK.
//
// A removed module, and an update that moved a module's database, leave the
// old database on the server with the module's data. The server lists them
// until the operator purges one, which cannot be undone, so the page asks the
// person to type the instance's name before it sends the purge.
import type { components, PrusaConnect } from '@prusa/connect';

export type LeftDatabase = components['schemas']['ModuleDatabaseLeft'];
export type PurgedDatabase = components['schemas']['ModuleDatabasePurged'];

/** What this store needs of the SDK, so a test hands in a fake (svelte-data.md). */
export type LeftDatabasesClient = Pick<
  PrusaConnect,
  'listModuleDatabases' | 'purgeModuleDatabase'
>;

/** The kind and the name an instance's kind:name holds. */
export const instanceParts = (
  instance: string,
): { kind: string; name: string } | undefined => {
  const cut = instance.indexOf(':');
  if (cut <= 0 || cut === instance.length - 1) return undefined;

  return { kind: instance.slice(0, cut), name: instance.slice(cut + 1) };
};

export class LeftDatabases {
  readonly #client: LeftDatabasesClient;
  // The server owns the whole list, so it is replaced whole on every read
  // (svelte-5.md).
  #items = $state.raw<readonly LeftDatabase[]>([]);
  #loaded = $state(false);
  #error = $state<unknown>(undefined);
  #purging = $state<string | undefined>(undefined);
  #purgeError = $state<unknown>(undefined);
  #purged = $state.raw<readonly PurgedDatabase[]>([]);
  // One flight per piece of data (svelte-data.md).
  #flight: Promise<void> | undefined;
  // Counts the resets. A request that started before a reset records nothing.
  #resets = 0;

  constructor(client: LeftDatabasesClient) {
    this.#client = client;
  }

  /** Every database left behind this person administers, oldest first. */
  get items(): readonly LeftDatabase[] {
    return this.#items;
  }

  /** True once a read has answered, whether it found rows or none. */
  get loaded(): boolean {
    return this.#loaded;
  }

  /** The last refusal of a read, as it was thrown. */
  get error(): unknown {
    return this.#error;
  }

  /** The instance whose purge is in flight, and undefined for none. */
  get purging(): string | undefined {
    return this.#purging;
  }

  /** The last refusal of a purge, as it was thrown. */
  get purgeError(): unknown {
    return this.#purgeError;
  }

  /** What the last purge did, until the next one starts. */
  get purged(): readonly PurgedDatabase[] {
    return this.#purged;
  }

  load(): Promise<void> {
    this.#flight ??= this.#read().finally(() => {
      this.#flight = undefined;
    });

    return this.#flight;
  }

  /**
   * Purges every database left behind under one instance. `confirm` is what
   * the person typed, sent as it is: the server purges nothing unless it is
   * the instance again.
   */
  async purge(instance: string, confirm: string): Promise<boolean> {
    const parts = instanceParts(instance);
    if (parts === undefined || this.#purging !== undefined) return false;

    const resets = this.#resets;
    this.#purging = instance;
    this.#purgeError = undefined;
    this.#purged = [];

    try {
      const answer = await this.#client.purgeModuleDatabase({
        path: parts,
        query: { confirm },
      });
      if (resets !== this.#resets) return false;
      this.#purged = answer.items ?? [];
      await this.#read();

      return true;
    } catch (thrown) {
      if (resets === this.#resets) this.#purgeError = thrown;

      return false;
    } finally {
      if (resets === this.#resets) this.#purging = undefined;
    }
  }

  reset(): void {
    this.#resets++;
    this.#items = [];
    this.#loaded = false;
    this.#error = undefined;
    this.#purging = undefined;
    this.#purgeError = undefined;
    this.#purged = [];
    this.#flight = undefined;
  }

  async #read(): Promise<void> {
    const resets = this.#resets;

    try {
      const answer = await this.#client.listModuleDatabases();
      if (resets !== this.#resets) return;
      this.#items = answer.items ?? [];
      this.#error = undefined;
      this.#loaded = true;
    } catch (thrown) {
      if (resets === this.#resets) this.#error = thrown;
    }
  }
}
