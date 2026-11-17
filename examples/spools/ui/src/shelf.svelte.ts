// What the page reads and what it writes through.
//
// A class, because it is state the page shares with itself across a read and a
// write (svelte-5.md). The service comes in through the constructor, which is
// what lets the test drive it without a network.
import type { Spool, SpoolsService } from './spools';

/** Why a call failed, in the words the page shows. */
const reasonOf = (thrown: unknown): string =>
  thrown instanceof Error ? thrown.message : String(thrown);

/**
 * The shelf, as one screen sees it: the rows, whether a call is in flight, and
 * why the last one failed.
 *
 * `rows` is `$state.raw`: the module replaces the whole list on every read and
 * never edits a row in place, so deep proxying each one buys nothing
 * (svelte-5.md).
 */
export class Shelf {
  readonly #spools: SpoolsService;
  /** Who the host says is signed in. It is read once and never changes. */
  readonly who: string;
  #rows = $state.raw<readonly Spool[]>([]);
  #busy = $state(false);
  #loaded = $state(false);
  #problem = $state('');
  /** The read in flight, so two components asking at once make one request. */
  #flight: Promise<void> | undefined;

  constructor(spools: SpoolsService, who: string) {
    this.#spools = spools;
    this.who = who;
  }

  /** The shelf, newest last. Read-only by design: a write goes through `add`. */
  get rows(): readonly Spool[] {
    return this.#rows;
  }

  /** A call is in flight. The page reports it on the control that started it. */
  get busy(): boolean {
    return this.#busy;
  }

  /** False until the first read answered, which is not the same as an empty shelf. */
  get loaded(): boolean {
    return this.#loaded;
  }

  /** Why the last call failed, and empty when none did. */
  get problem(): string {
    return this.#problem;
  }

  /**
   * Reads the shelf. A second call while the first is in flight is handed the
   * first one's promise, so one screen makes one request for one datum
   * (svelte-data.md).
   */
  read(): Promise<void> {
    this.#flight ??= this.#read().finally(() => {
      this.#flight = undefined;
    });

    return this.#flight;
  }

  async #read(): Promise<void> {
    this.#busy = true;
    this.#problem = '';

    try {
      this.#rows = await this.#spools.list();
      this.#loaded = true;
    } catch (thrown) {
      this.#problem = reasonOf(thrown);
    } finally {
      this.#busy = false;
    }
  }

  /**
   * Adds one spool and puts the row the module answered on the shelf, rather
   * than the row the page sent: the module owns what a spool is, and it may
   * have trimmed or filled in a field.
   *
   * It reports whether the write landed, so the page can clear the form on a
   * write that did and keep what was typed on one that did not.
   */
  async add(row: Spool): Promise<boolean> {
    this.#busy = true;
    this.#problem = '';

    try {
      this.#rows = [...this.#rows, await this.#spools.add(row)];

      return true;
    } catch (thrown) {
      this.#problem = reasonOf(thrown);

      return false;
    } finally {
      this.#busy = false;
    }
  }
}

// The one shelf this bundle's page reads.
//
// A module-level holder rather than Svelte context, because the setup function
// runs before any component of this module is mounted, so there is no
// component for `setContext` to hang on. It is set once, in `index.ts`, before
// the page can be opened: a bundle is imported once per page, and a module is
// registered once per import (ADR-20260911-4p6 rule 25).
let held: Shelf | undefined;

/** Called once by the module's setup function, with the host's signal. */
export const holdShelf = (shelf: Shelf, signal: AbortSignal): void => {
  // A setup that was still running when the person left resumes with a
  // signal that already fired. What it built is not held.
  if (signal.aborted) return;
  held = shelf;
  // What the person who admitted this module read is let go when they leave.
  signal.addEventListener(
    'abort',
    () => {
      if (held === shelf) held = undefined;
    },
    { once: true },
  );
};

/** The shelf the page reads. It throws rather than drawing an empty screen. */
export const heldShelf = (): Shelf => {
  if (!held) {
    throw new Error('the spools module drew a page before its setup ran');
  }

  return held;
};
