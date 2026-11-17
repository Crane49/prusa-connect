// The printers this server hears on the local network, for the printer page.
// Being on the list grants a printer nothing. It is a name and a link to its
// own page.
import { type DataFault, faultOf } from './fault';
import {
  type DiscoveryClient,
  type NearbyPrinter,
  readNearby,
} from './services/discovery';
import { systemTimers, type Timers } from './timers';

/** How the store is built beyond its client. */
export interface NearbyPrintersOptions {
  /** The wait between reads while the page shows the list, in milliseconds. */
  readonly refreshMs?: number;
  readonly timers?: Timers;
}

const DEFAULT_REFRESH_MS = 15_000;

/**
 * A shared store, so a class (svelte-5.md). The client comes in through the
 * constructor, which lets a test pass a fake.
 *
 * A printer joins and leaves the list without an event, so the page reads the
 * list again on a tick while it shows it. `listen()` starts the tick and
 * returns the function that ends it.
 */
export class NearbyPrinters {
  readonly #client: DiscoveryClient;
  readonly #timers: Timers;
  readonly #refreshMs: number;

  #items = $state.raw<readonly NearbyPrinter[]>([]);
  #loading = $state(false);
  #loaded = $state(false);
  #error = $state<DataFault | undefined>(undefined);
  #flight: Promise<void> | undefined;
  #handle: number | undefined;
  // True between listen() and stop(). A read still in flight when stop() runs
  // reads it when it answers, and schedules nothing.
  #listening = false;
  // Counts the resets, so a read that started before one records nothing.
  #resets = 0;

  constructor(client: DiscoveryClient, options: NearbyPrintersOptions = {}) {
    this.#client = client;
    this.#timers = options.timers ?? systemTimers;
    this.#refreshMs = options.refreshMs ?? DEFAULT_REFRESH_MS;
  }

  /** Read-only by design: the list is the server's (svelte-5.md). */
  get items(): readonly NearbyPrinter[] {
    return this.#items;
  }

  get loading(): boolean {
    return this.#loading;
  }

  /** True once a read has answered, which tells an empty list from an unread one. */
  get loaded(): boolean {
    return this.#loaded;
  }

  get error(): DataFault | undefined {
    return this.#error;
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

  /**
   * Forgets the list. The app calls it when the person signed in changes, so
   * the next person never sees what the last one could read. A read that
   * started before the reset records nothing.
   */
  reset(): void {
    this.#resets += 1;
    this.#flight = undefined;
    this.#items = [];
    this.#loading = false;
    this.#loaded = false;
    this.#error = undefined;
  }

  /** Reads the list every `refreshMs` until the returned function is called. */
  listen(): () => void {
    this.#listening = true;
    this.#schedule();

    return () => {
      this.stop();
    };
  }

  /** Stops the refresh tick, including one a read in flight would start. */
  stop(): void {
    this.#listening = false;
    this.#clear();
  }

  #clear(): void {
    if (this.#handle === undefined) return;

    this.#timers.clear(this.#handle);
    this.#handle = undefined;
  }

  // The tick goes through load(), so a tick that fires while the page's own
  // read is in flight joins it instead of sending a second one. A refusal
  // with 403 does not change until the person signs in again, so the tick
  // stops asking after one.
  #schedule(): void {
    this.#clear();
    if (!this.#listening || this.#error?.status === 403) return;

    this.#handle = this.#timers.set(() => {
      this.#handle = undefined;
      void this.load().finally(() => {
        this.#schedule();
      });
    }, this.#refreshMs);
  }

  async #read(): Promise<void> {
    const resets = this.#resets;
    this.#loading = true;
    try {
      const page = await readNearby(this.#client);
      if (resets !== this.#resets) return;
      this.#items = page.items ?? [];
      this.#loaded = true;
      this.#error = undefined;
    } catch (thrown) {
      if (resets !== this.#resets) return;
      this.#error = faultOf(thrown);
    } finally {
      if (resets === this.#resets) this.#loading = false;
    }
  }
}
