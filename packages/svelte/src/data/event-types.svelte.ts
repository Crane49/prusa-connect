import {
  type EventTypeEntry,
  type EventTypesClient,
  readEventTypes,
} from './services/event-types';

/**
 * What each event type means, in the server's own words.
 *
 * A stored event carries its reverse-DNS type and nothing else, and
 * `com.prusa.connect.device.state.changed` is an identifier rather than a
 * sentence. The server publishes a summary for every type it can emit, so a
 * log reads it instead of inventing its own wording per screen.
 *
 * A shared store, so a class (svelte-5.md). The vocabulary belongs to the
 * server and not to the build. A page reads it when it needs it, and keeps it
 * while the page stays open.
 */
export class EventTypes {
  readonly #client: EventTypesClient;
  #entries = $state.raw<readonly EventTypeEntry[]>([]);
  #asked = $state(false);
  #flight: Promise<void> | undefined;

  constructor(client: EventTypesClient) {
    this.#client = client;
  }

  /** Every type the server declared, in the order it published them. */
  get entries(): readonly EventTypeEntry[] {
    return this.#entries;
  }

  /** True once a read has finished, whether it answered or failed. */
  get asked(): boolean {
    return this.#asked;
  }

  /**
   * The one-line summary of a type, or the type itself where the server
   * published none. A log that fell back to nothing would lose the row.
   */
  summaryOf(type: string): string {
    return this.#entries.find((one) => one.type === type)?.summary ?? type;
  }

  /** Reads the vocabulary. A second call joins the first (svelte-data.md). */
  read(): Promise<void> {
    this.#flight ??= this.#read().finally(() => {
      this.#flight = undefined;
    });

    return this.#flight;
  }

  async #read(): Promise<void> {
    try {
      const answer = await readEventTypes(this.#client);
      this.#entries = answer.items ?? [];
    } catch {
      // A server that cannot answer this still draws the log: every row
      // falls back to its own type, which is what it drew before.
      this.#entries = [];
    } finally {
      this.#asked = true;
    }
  }
}
