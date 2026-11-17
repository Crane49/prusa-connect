// The folders other Fractal servers share with this one: the servers above it
// and below it, read through the Fractal module's own routes (ADR-20261023-7r3
// rule 18 and R2). A content store is one storage of another server. Its
// listings are named by the hash of what they hold, so a listing once read
// never changes and is kept.
import { ApiError } from '@prusa/connect';
import type { components, Operations } from 'fractal-client';

import { log } from '../log';

const storesLog = log.scope('fractal-stores');

export type StoreEntry = components['schemas']['StoreEntry'];
type StoreInterface = components['schemas']['Interface'];

/** What this store needs of the module's client, so a test hands in a fake (svelte-data.md). */
export type FractalStoresClient = Pick<
  Operations,
  'listFractalStores' | 'readFractalStoreListing'
>;

const CONTENT_STORE = 'ContentStore1';

// ponytail: one page of stores. A tree with more than 500 shared storages
// lists the first 500; follow `next` when one does.
const STORE_PAGE = 500;

/** One storage another server shares from, as this server last heard of it. */
export interface SharedStore {
  /** The server's place in the tree, such as hq/brno. */
  readonly node: string;
  /** The storage, by the name the server declares it under. */
  readonly instance: string;
  /** The identity of its top listing, undefined until the server first scanned it. */
  readonly root: string | undefined;
  /** When the server was last heard from, set only while its report may be out of date. */
  readonly staleSince: number | undefined;
}

/** A folder of one store, which is where the reader stands below the top. */
export interface SharedPlace {
  /** The server's place in the tree, such as hq/brno. */
  readonly node: string;
  /** The storage, by the name the server declares it under. */
  readonly instance: string;
  /** The path from the root of the storage, such as projects/fixtures. */
  readonly path: string;
}

/** One file or folder of a listing, with the store that holds it. */
export interface SharedEntry {
  readonly store: SharedStore;
  /** The path from the root of the storage, such as projects/fixtures. */
  readonly path: string;
  readonly name: string;
  readonly folder: boolean;
  /** The length of a file. */
  readonly size: number;
  /** What names the bytes of a file, or the listing of a folder. */
  readonly identity: string;
}

const storeOf = (item: StoreInterface): SharedStore => {
  const root = item.props?.rootHash;

  return {
    node: item.node,
    instance: item.instance,
    root: typeof root === 'string' ? root : undefined,
    staleSince: item.status === 'STALE' ? item.updated_at : undefined,
  };
};

const entriesOf = (
  store: SharedStore,
  at: string,
  entries: readonly StoreEntry[],
): SharedEntry[] =>
  entries.map((entry) => ({
    store,
    path: at === '' ? entry.name : `${at}/${entry.name}`,
    name: entry.name,
    folder: entry.kind === 'directory',
    size: entry.size,
    identity: entry.identity,
  }));

/** The place of a folder, which is what `goTo` takes. */
export const placeOfFolder = (folder: SharedEntry): SharedPlace => ({
  node: folder.store.node,
  instance: folder.store.instance,
  path: folder.path,
});

const samePlace = (
  a: SharedPlace | undefined,
  b: SharedPlace | undefined,
): boolean =>
  a?.node === b?.node && a?.instance === b?.instance && a?.path === b?.path;

export class FractalStores {
  readonly #client: FractalStoresClient;
  #node = $state('');
  // The server owns these lists, so each is replaced whole (svelte-5.md).
  #stores = $state.raw<readonly SharedStore[]>([]);
  #rows = $state.raw<readonly SharedEntry[]>([]);
  #trail = $state.raw<readonly SharedEntry[]>([]);
  #unread = $state.raw<readonly SharedStore[]>([]);
  #loading = $state(false);
  #loaded = $state(false);
  #error = $state<unknown>(undefined);
  // The listings already read, by identity. Nothing draws the map itself.
  // eslint-disable-next-line svelte/prefer-svelte-reactivity
  readonly #listings = new Map<string, readonly StoreEntry[]>();
  // One flight per piece of data (svelte-data.md).
  #flight: Promise<void> | undefined;
  // Stops the read of a folder the reader has left.
  #walk: AbortController | undefined;
  // Counts the resets. A request that started before a reset records nothing.
  #resets = 0;

  constructor(client: FractalStoresClient) {
    this.#client = client;
  }

  /** This server's own place in the tree, empty before a read answered. */
  get node(): string {
    return this.#node;
  }

  /** Every storage another server shares from, in the order of the tree. */
  get stores(): readonly SharedStore[] {
    return this.#stores;
  }

  /**
   * The rows on screen: the top of every store, or the folder the reader
   * opened. Read-only by design: the reader moves with `goTo`.
   */
  get rows(): readonly SharedEntry[] {
    return this.#rows;
  }

  /** The folders the reader opened, outermost first. Empty at the top. */
  get trail(): readonly SharedEntry[] {
    return this.#trail;
  }

  /** The folder on screen, and undefined at the top. */
  get place(): SharedPlace | undefined {
    const folder = this.#trail.at(-1);

    return folder === undefined ? undefined : placeOfFolder(folder);
  }

  /** The stores whose top listing could not be read. */
  get unread(): readonly SharedStore[] {
    return this.#unread;
  }

  get loading(): boolean {
    return this.#loading;
  }

  /** True once a read has answered, whether it found stores or none. */
  get loaded(): boolean {
    return this.#loaded;
  }

  /** The last refusal, as it was thrown. */
  get error(): unknown {
    return this.#error;
  }

  /** Reads the stores and the top of each, and goes back to the top. */
  load(): Promise<void> {
    this.#flight ??= this.#read().finally(() => {
      this.#flight = undefined;
    });

    return this.#flight;
  }

  /**
   * Shows the folder a place names, read down from the top of its store, and
   * the top for none. The place comes from the address, so the back button
   * walks the shared folders too. A folder that is gone, or a store this
   * server no longer hears of, shows the deepest folder still there.
   */
  async goTo(place: SharedPlace | undefined): Promise<void> {
    this.#walk?.abort();
    const store = this.#stores.find(
      (one) => one.node === place?.node && one.instance === place.instance,
    );
    if (samePlace(place, this.place) || store?.root === undefined) {
      this.#loading = false;
      if (store?.root === undefined && this.#trail.length > 0) {
        this.#trail = [];
        this.#rows = this.#topRows();
      }
      this.#error = undefined;

      return;
    }

    const walk = new AbortController();
    this.#walk = walk;
    const resets = this.#resets;
    this.#loading = true;
    try {
      const trail: SharedEntry[] = [];
      let entries = await this.#listing(store, store.root, walk.signal);
      for (const name of place?.path.split('/') ?? []) {
        const folder = entriesOf(store, trail.at(-1)?.path ?? '', entries).find(
          (entry) => entry.folder && entry.name === name,
        );
        if (folder === undefined) break;
        entries = await this.#listing(store, folder.identity, walk.signal);
        trail.push(folder);
      }
      if (resets !== this.#resets || walk.signal.aborted) return;
      const folder = trail.at(-1);
      this.#trail = trail;
      this.#rows =
        folder === undefined
          ? this.#topRows()
          : entriesOf(store, folder.path, entries);
      this.#error = undefined;
    } catch (thrown) {
      if (resets !== this.#resets || walk.signal.aborted) return;
      this.#error = thrown;
    } finally {
      if (resets === this.#resets && !walk.signal.aborted) {
        this.#loading = false;
      }
    }
  }

  reset(): void {
    this.#resets++;
    this.#walk?.abort();
    this.#node = '';
    this.#stores = [];
    this.#rows = [];
    this.#trail = [];
    this.#unread = [];
    this.#loading = false;
    this.#loaded = false;
    this.#error = undefined;
    this.#listings.clear();
    this.#flight = undefined;
  }

  async #listing(
    store: SharedStore,
    identity: string,
    signal?: AbortSignal,
  ): Promise<readonly StoreEntry[]> {
    const held = this.#listings.get(identity);
    if (held !== undefined) return held;

    const answer = await this.#client.readFractalStoreListing(
      { path: { node: store.node, instance: store.instance, identity } },
      signal,
    );
    this.#listings.set(identity, answer.entries);

    return answer.entries;
  }

  #topRows(): SharedEntry[] {
    return this.#stores.flatMap((store) => {
      const top =
        store.root === undefined ? undefined : this.#listings.get(store.root);

      return top === undefined ? [] : entriesOf(store, '', top);
    });
  }

  async #read(): Promise<void> {
    const resets = this.#resets;
    this.#walk?.abort();
    this.#loading = true;
    try {
      const answer = await this.#client.listFractalStores({
        query: { limit: STORE_PAGE },
      });
      if (resets !== this.#resets) return;
      const stores = answer.items
        .filter(
          (item) =>
            item.interface === CONTENT_STORE && item.node !== answer.node,
        )
        .map(storeOf);

      // A store above that does not answer leaves the others on screen.
      const unread: SharedStore[] = [];
      await Promise.all(
        stores.map(async (store) => {
          if (store.root === undefined) return;
          try {
            await this.#listing(store, store.root);
          } catch (thrown) {
            unread.push(store);
            storesLog.warn('a shared storage did not answer', {
              node: store.node,
              instance: store.instance,
              code: thrown instanceof ApiError ? thrown.code : undefined,
            });
          }
        }),
      );
      if (resets !== this.#resets) return;

      this.#node = answer.node;
      this.#stores = stores;
      this.#unread = stores.filter((store) => unread.includes(store));
      this.#trail = [];
      this.#rows = this.#topRows();
      this.#error = undefined;
      this.#loaded = true;
    } catch (thrown) {
      if (resets !== this.#resets) return;
      this.#error = thrown;
      this.#loaded = true;
    } finally {
      if (resets === this.#resets) this.#loading = false;
    }
  }
}
