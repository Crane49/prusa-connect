// One printer's stored events, a page at a time.
//
// A fact is a type, an instant, an origin and an opaque payload. A log is a
// table and a table pages, which is why this is a store of its own and not a
// member of the store that reads the measurements: tying a table's paging to
// a chart's window would make one wait on the other.
import { type DataFault, faultOf } from './fault';
import {
  type HistoryClient,
  type HistoryFact,
  type HistoryWindow,
  readDeviceFacts,
} from './services/history';

/** How many rows one page asks for. */
export const FACTS_PAGE_LIMIT = 200;

/** One printer's stored events. A shared store, so a class (svelte-5.md). */
export class DeviceFacts {
  readonly #client: HistoryClient;
  readonly #deviceId: string;

  // Replaced whole rather than pushed into, so one page is one recomputation
  // for its readers and not one per row (svelte-data.md).
  #rows = $state.raw<readonly HistoryFact[]>([]);
  #cursor = $state('');
  #incomplete = $state(false);
  #loading = $state(false);
  #loaded = $state(false);
  #error = $state<DataFault | undefined>(undefined);

  #flight: Promise<void> | undefined;
  #reading: AbortController | undefined;

  constructor(client: HistoryClient, deviceId: string) {
    this.#client = client;
    this.#deviceId = deviceId;
  }

  get rows(): readonly HistoryFact[] {
    return this.#rows;
  }

  get loading(): boolean {
    return this.#loading;
  }

  /** Whether a read has finished, whether it answered rows or a refusal. */
  get loaded(): boolean {
    return this.#loaded;
  }

  get error(): DataFault | undefined {
    return this.#error;
  }

  /** Whether the server knows it did not see this window whole. */
  get incomplete(): boolean {
    return this.#incomplete;
  }

  /** Whether a page is waiting behind the one that was read. */
  get more(): boolean {
    return this.#cursor !== '';
  }

  /** Reads the first page, dropping whatever was held. */
  load(window: HistoryWindow): Promise<void> {
    return this.#read(window, true);
  }

  /** Appends the page the last cursor points at. */
  loadMore(window: HistoryWindow): Promise<void> {
    if (this.#cursor === '') return Promise.resolve();

    return this.#read({ ...window, cursor: this.#cursor }, false);
  }

  /** Abandons a read in flight, for a page moving to another printer. */
  abort(): void {
    this.#reading?.abort();
    this.#reading = undefined;
    // Dropped now, not in the aborted fetch's `.finally()`: that runs on a
    // later microtask, and a `load()` called in the same tick (as a Svelte
    // effect's cleanup-then-rerun does) must not be handed the stale flight.
    this.#flight = undefined;
  }

  #read(window: HistoryWindow, fresh: boolean): Promise<void> {
    // A second call while one read is in flight is given that read.
    if (this.#flight) return this.#flight;

    this.abort();
    const reading = new AbortController();
    this.#reading = reading;
    this.#loading = true;

    const flight = this.#fetch(window, fresh, reading.signal).finally(() => {
      // An abandoned flight's own completion must not report the newer
      // flight it was replaced by as finished.
      if (this.#flight === flight) {
        this.#flight = undefined;
        this.#loading = false;
        this.#loaded = true;
      }
      if (this.#reading === reading) this.#reading = undefined;
    });
    this.#flight = flight;

    return flight;
  }

  async #fetch(
    window: HistoryWindow,
    fresh: boolean,
    signal: AbortSignal,
  ): Promise<void> {
    try {
      const answer = await readDeviceFacts(
        this.#client,
        this.#deviceId,
        window,
        signal,
      );

      this.#rows = fresh
        ? (answer.items ?? [])
        : [...this.#rows, ...(answer.items ?? [])];
      this.#cursor = answer.cursor;
      this.#incomplete = answer.incomplete;
      this.#error = undefined;
    } catch (thrown) {
      if (signal.aborted) return;
      this.#error = faultOf(thrown);
      if (fresh) this.#rows = [];
    }
  }
}
