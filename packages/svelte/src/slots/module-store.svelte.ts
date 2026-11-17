// A module's own key-value store, scoped to its namespace (ADR-20260911-4p6 rule 18).
//
// The records live in the core, under
// `/api/v1/modules/<namespace>/state`, one per key and per signed-in person.
// Every call goes through the SDK client the host carries, so the page reaches
// the core the one way `frontend-structure.md` allows and the core authorizes
// each request on the module's instance.
//
// `localStorage` is the cache between a load and the answer, and nothing else.
// The surface below is synchronous, so a settings pane that asks for a value
// while the first read is in flight has to be given something: it is given what
// the last answer left in the cache. The answer replaces it.
//
// Everyone who signs in on one browser shares its `localStorage`. So the cache
// is kept under the person's id as well as the namespace, and the app removes
// every cached record when a person signs out or another person signs in.
import type { PrusaConnect } from '@prusa/connect';

/**
 * What the store needs of the SDK. Naming only the four operations is what
 * lets a test hand it a stub without writing out every operation the core
 * declares (svelte-data.md).
 */
export type ModuleStateClient = Pick<
  PrusaConnect,
  'listModuleState' | 'getModuleState' | 'putModuleState' | 'deleteModuleState'
>;

/** What the store needs of `localStorage`. */
export interface ModuleStorage {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
  readonly length: number;
  key(index: number): string | null;
}

/**
 * What a module may do with its own records. Values are strings: a module
 * that keeps a structure encodes it, so the shape stays the module's and the
 * host stores bytes.
 */
export interface ScopedStore {
  get(key: string): string | undefined;
  set(key: string, value: string): void;
  delete(key: string): void;
  /** The keys this module has written, sorted, without the namespace prefix. */
  keys(): readonly string[];
}

/** The start of every key a module store caches under. */
const CACHE_PREFIX = 'module:';

/**
 * The prefix every cached record of one module is written under, for one
 * person. The person's id is encoded, so the colon in `user:ada` cannot be
 * read as the end of the prefix.
 */
export const storeKeyPrefix = (namespace: string, person?: string): string =>
  person === undefined
    ? `${CACHE_PREFIX}${namespace}:`
    : `${CACHE_PREFIX}${namespace}:${encodeURIComponent(person)}:`;

/**
 * The longest key the core accepts, and the alphabet it accepts. It is one
 * DNS-1123 label, the grammar ADR-20260911-4p6 rule 14 holds a page path and a
 * contributed id to, so a module author writes one kind of name.
 */
const KEY_PATTERN = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/;

/** True for a key the core will accept, so a doomed request is never sent. */
export const isStoreKey = (key: string): boolean => KEY_PATTERN.test(key);

// The DOM lib declares `localStorage` as always present. Under SSR it is not,
// and a browser set to block site data throws on the read rather than
// answering null (see `styles/theme.svelte.ts`).
const browser: { localStorage?: ModuleStorage } = globalThis;

const browserStorage = (): ModuleStorage | null => {
  try {
    return browser.localStorage ?? null;
  } catch {
    return null;
  }
};

/**
 * Removes every record any module store cached in this browser, for every
 * person. The app calls it when a person signs out or another person signs
 * in, so nothing one person's modules kept is left for the next person.
 */
export const forgetModuleCaches = (
  storage: ModuleStorage | null = browserStorage(),
): void => {
  if (!storage) return;

  try {
    const cached: string[] = [];
    for (let index = 0; index < storage.length; index += 1) {
      const key = storage.key(index);
      if (key?.startsWith(CACHE_PREFIX)) cached.push(key);
    }
    for (const key of cached) storage.removeItem(key);
  } catch {
    // Site data blocked: nothing was cached.
  }
};

const asError = (thrown: unknown): Error =>
  thrown instanceof Error ? thrown : new Error(String(thrown));

/**
 * The stored document as a string. The core keeps any JSON, so a value another
 * client wrote as an object is rendered back to the string surface rather than
 * dropped.
 */
const asText = (value: unknown): string =>
  typeof value === 'string' ? value : JSON.stringify(value);

export interface ModuleStoreOptions {
  /** The SDK client the host carries. Without one the store stays local. */
  client?: ModuleStateClient;
  /** `null` remembers nothing, which is what a server render gets. */
  storage?: ModuleStorage | null;
  /**
   * The person the records belong to, such as `user:ada`. The cache is kept
   * under it, so two people on one browser never read each other's records.
   */
  person?: string;
}

/**
 * A shared store, so a class (svelte-5.md). The records are held in a rune as
 * well as in the cache, so a settings pane that writes one redraws without
 * re-reading the browser and without waiting for the core.
 *
 * A write is applied to the rune first and sent after, so a pane redraws at
 * once. A refusal puts the record back as it was and lands in `error`, which
 * is what a pane shows when the core would not keep a value.
 */
export class ModuleStore implements ScopedStore {
  readonly #namespace: string;
  readonly #prefix: string;
  readonly #storage: ModuleStorage | null;
  readonly #client: ModuleStateClient | undefined;
  #records = $state<Record<string, string>>({});
  #loaded = $state(false);
  #error = $state<Error | undefined>(undefined);
  #flight: Promise<void>;
  #closed = false;

  constructor(
    namespace: string,
    { client, storage, person }: ModuleStoreOptions = {},
  ) {
    this.#namespace = namespace;
    this.#prefix = storeKeyPrefix(namespace, person);
    this.#storage = storage === undefined ? browserStorage() : storage;
    this.#client = client;
    this.#records = this.#cached();
    this.#flight = client ? this.#load(client) : Promise.resolve();
  }

  /** The namespace the records are scoped to. Read-only by design. */
  get namespace(): string {
    return this.#namespace;
  }

  /** False until the core has answered, so a pane can wait rather than flash. */
  get loaded(): boolean {
    return this.#loaded;
  }

  /** The last refusal, for a settings pane to show. */
  get error(): Error | undefined {
    return this.#error;
  }

  /** Resolves once the first read has answered. A second call is handed it. */
  ready(): Promise<void> {
    return this.#flight;
  }

  get(key: string): string | undefined {
    return this.#records[key];
  }

  /**
   * Rejects a key the core would not accept: a record nothing can name cannot
   * be read back, and a doomed request is not worth sending.
   */
  set(key: string, value: string): void {
    // A closed store belongs to the person who left, and a write would reach
    // the server with the next person's cookie.
    if (this.#closed) return;
    if (!isStoreKey(key)) {
      this.#error = new Error(
        `${key === '' ? 'an empty key' : `the key ${key}`} is not one label of lowercase letters, digits and inner hyphens`,
      );

      return;
    }

    if (this.#records[key] === value) return;

    const previous = this.#records[key];
    this.#records[key] = value;
    this.#cache(key, value);

    void this.#send(
      () =>
        this.#client?.putModuleState({
          path: { namespace: this.#namespace, key },
          body: { value },
        }),
      () => {
        this.#restore(key, previous);
      },
    );
  }

  delete(key: string): void {
    if (this.#closed || !(key in this.#records)) return;

    const previous = this.#records[key];
    this.#forget(key);

    void this.#send(
      () =>
        this.#client?.deleteModuleState({
          path: { namespace: this.#namespace, key },
        }),
      () => {
        this.#restore(key, previous);
      },
    );
  }

  keys(): readonly string[] {
    return Object.keys(this.#records).sort();
  }

  /**
   * Stops the store. The loader calls it when the person changes. An answer
   * that arrives later is not written to the records or to the cache, because
   * it belongs to the person who left.
   */
  close(): void {
    this.#closed = true;
  }

  /** Reads the keys the core holds, then each value, and replaces the cache. */
  async #load(client: ModuleStateClient): Promise<void> {
    try {
      const listing = await client.listModuleState({
        path: { namespace: this.#namespace },
      });

      const records: Record<string, string> = {};

      await Promise.all(
        (listing.items ?? []).map(async (key) => {
          const held = await client.getModuleState({
            path: { namespace: this.#namespace, key },
          });

          records[key] = asText(held.value);
        }),
      );
      if (this.#closed) return;

      this.#records = records;
      this.#error = undefined;
      this.#recache(records);
    } catch (thrown) {
      // The cache stands. It is this person's own cache, so a pane that was
      // drawing the last answer keeps drawing it, and the reason is here for
      // the pane to show.
      if (!this.#closed) this.#error = asError(thrown);
    } finally {
      this.#loaded = true;
    }
  }

  /** Sends one write, and undoes it here when the core refuses. */
  async #send(
    call: () => Promise<unknown> | undefined,
    undo: () => void,
  ): Promise<void> {
    if (this.#closed) return;
    const flight = call();
    if (!flight) return;

    try {
      await flight;
      if (!this.#stopped()) this.#error = undefined;
    } catch (thrown) {
      if (this.#stopped()) return;
      undo();
      this.#error = asError(thrown);
    }
  }

  // Read again after an await: `close` may have run while the request was
  // on its way.
  #stopped(): boolean {
    return this.#closed;
  }

  /** Puts a record back as it was, which is what a refused write leaves. */
  #restore(key: string, previous: string | undefined): void {
    if (previous === undefined) {
      this.#forget(key);

      return;
    }

    this.#records[key] = previous;
    this.#cache(key, previous);
  }

  #forget(key: string): void {
    this.#records = Object.fromEntries(
      Object.entries(this.#records).filter(([name]) => name !== key),
    );

    try {
      this.#storage?.removeItem(this.#prefix + key);
    } catch {
      // A browser with site data blocked still runs the module; it forgets.
    }
  }

  #cache(key: string, value: string): void {
    if (this.#closed) return;
    try {
      this.#storage?.setItem(this.#prefix + key, value);
    } catch {
      // As above: the record is in the rune and in the core either way.
    }
  }

  /** Rewrites the cache to be exactly what the core answered. */
  #recache(records: Record<string, string>): void {
    const storage = this.#storage;
    if (!storage) return;

    try {
      for (const key of Object.keys(this.#cached())) {
        if (!(key in records)) storage.removeItem(this.#prefix + key);
      }

      for (const [key, value] of Object.entries(records)) {
        storage.setItem(this.#prefix + key, value);
      }
    } catch {
      // Site data blocked: the next load reads the core again.
    }
  }

  #cached(): Record<string, string> {
    const records: Record<string, string> = {};
    const storage = this.#storage;
    if (!storage) return records;

    try {
      for (let index = 0; index < storage.length; index += 1) {
        const full = storage.key(index);
        if (!full?.startsWith(this.#prefix)) continue;
        // A key is one label with no colon. A longer rest is another
        // person's record under this namespace, which is not this store's.
        if (full.slice(this.#prefix.length).includes(':')) continue;

        const value = storage.getItem(full);
        if (value !== null) records[full.slice(this.#prefix.length)] = value;
      }
    } catch {
      // Site data blocked: the module starts with nothing remembered.
    }

    return records;
  }
}

/** One store per namespace, built by the loader as it admits a module. */
export const createModuleStore = (
  namespace: string,
  options?: ModuleStoreOptions,
): ModuleStore => new ModuleStore(namespace, options);
