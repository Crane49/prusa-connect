// Which folders of the tree a person closed.
//
// A class, because the tree and every row of it share it (svelte-5.md). The
// module's own store comes in through the constructor, so the closed folders
// are kept for the person across reloads and a test hands in a fake.
//
// Every folder is open until the person closes it. A closed folder records the
// folder that was on screen when it was closed: while the page is somewhere
// inside a closed folder, the folder opens by itself so the reader sees where
// they are, unless they closed it from right there. A folder closed in an
// earlier visit was closed from nowhere, so it always opens on the way to the
// folder on screen.
//
// The closed folders are a plain record replaced whole on every change, and
// not a `SvelteMap`: `svelte/reactivity` is not in the host's import map, so
// importing it would put a second copy of the runtime into the bundle.
import type { ScopedStore } from '@prusa/connect-ui/slots';

/** The key the closed folders are kept under, for this person alone. */
const CLOSED_KEY = 'tree.closed';

/** The folder a closed folder was closed from, when it was closed in an earlier visit. */
const NOWHERE = -1;

/** Whether a value can be a folder id: 0 for the top, or a number the module gave. */
const isFolderId = (id: unknown): id is number =>
  Number.isInteger(id) && (id as number) >= 0;

/** The closed folders as the store keeps them: a list of ids. */
const readClosed = (store: ScopedStore): readonly number[] => {
  const kept = store.get(CLOSED_KEY);
  if (kept === undefined) return [];

  try {
    const ids: unknown = JSON.parse(kept);

    return Array.isArray(ids) ? ids.filter(isFolderId) : [];
  } catch {
    // A value somebody else wrote is not a reason to draw no tree.
    return [];
  }
};

export class TreeState {
  readonly #store: ScopedStore;
  /** Each closed folder, by its id, and the folder on screen when it was closed. */
  #closed = $state.raw<Readonly<Record<number, number>>>({});

  constructor(store: ScopedStore) {
    this.#store = store;
    this.#closed = Object.fromEntries(
      readClosed(store).map((id) => [id, NOWHERE]),
    );
  }

  /**
   * True when none of the folders named is closed. A folder deleted since it
   * was closed stays in the stored list, so the tree asks about the folders it
   * draws rather than about the list.
   */
  openAmong(ids: readonly number[]): boolean {
    return ids.every((id) => !Object.hasOwn(this.#closed, id));
  }

  /** True when no folder is closed. */
  get allOpen(): boolean {
    return Object.keys(this.#closed).length === 0;
  }

  /**
   * Whether one folder shows what is inside it. `holdsHere` is whether the
   * folder on screen is inside it, and `here` is that folder.
   */
  isOpen(id: number, here: number, holdsHere: boolean): boolean {
    if (!Object.hasOwn(this.#closed, id)) return true;
    const closedFrom = this.#closed[id];

    return holdsHere && closedFrom !== here;
  }

  /** Opens a closed folder, or closes an open one, from the folder on screen. */
  toggle(id: number, here: number, holdsHere: boolean): void {
    if (!isFolderId(id)) return;

    if (this.isOpen(id, here, holdsHere)) {
      this.#keep({ ...this.#closed, [id]: here });
    } else {
      this.#keep(
        Object.fromEntries(
          Object.entries(this.#closed).filter(([key]) => Number(key) !== id),
        ),
      );
    }
  }

  /** Closes every folder named, from the folder on screen, and opens the rest. */
  closeAll(ids: readonly number[], here: number): void {
    const closing = ids.filter(isFolderId);
    if (closing.length === 0 && this.allOpen) return;

    this.#keep(Object.fromEntries(closing.map((id) => [id, here])));
  }

  /** Opens every folder. */
  openAll(): void {
    if (this.allOpen) return;
    this.#keep({});
  }

  #keep(next: Readonly<Record<number, number>>): void {
    this.#closed = next;
    const ids = Object.keys(next).map(Number);
    if (ids.length === 0) this.#store.delete(CLOSED_KEY);
    else this.#store.set(CLOSED_KEY, JSON.stringify(ids));
  }
}
