// What one dashboard card shows: a few parts, read once when the card is
// drawn. A class, because the card and its test share it (svelte-5.md).
//
// A card reads when it is drawn and when its settings change, and never on a
// timer: the dashboard draws the card again when a person changes it, and a
// wall screen reloads the dashboard (ADR-20261008-hna rules 25 and 31).
import type { PartDetail, PartRow, PartsService, Picture } from '../parts';

/** One part as a card draws it. */
export interface CardPart {
  readonly id: number;
  readonly name: string;
  readonly picture: Picture | undefined;
  /** The newest version's number, and 0 for a part with none. */
  readonly version: number;
}

/** A part as a listing answers it, as a card draws it. */
export const cardPartOf = (row: PartRow): CardPart => ({
  id: row.id,
  name: row.name,
  picture: row.picture,
  version: row.latest?.number ?? 0,
});

/** A part as its own read answers it, as a card draws it. */
const cardPartOfDetail = (part: PartDetail): CardPart => ({
  id: part.id,
  name: part.name,
  picture: part.picture,
  version: part.last_version ?? 0,
});

/** Where a card reads from. */
export interface FolderQuery {
  readonly folder: number;
  readonly deep: boolean;
  readonly tag: string;
  readonly order: 'name' | 'changed';
  readonly limit: number;
}

export class CardParts {
  readonly #parts: PartsService;

  #rows = $state.raw<readonly CardPart[]>([]);
  #matched = $state(0);
  #missing = $state(0);
  #loading = $state(true);
  #fault = $state.raw<unknown>(undefined);
  /** The read on its way, so a newer one or the card leaving can drop it. */
  #abort: AbortController | undefined;

  constructor(parts: PartsService) {
    this.#parts = parts;
  }

  /** The parts, in the order the card draws them. */
  get rows(): readonly CardPart[] {
    return this.#rows;
  }

  /** How many parts the query matched, which may be more than the card shows. */
  get matched(): number {
    return this.#matched;
  }

  /** How many chosen parts no longer exist, or the person may not see. */
  get missing(): number {
    return this.#missing;
  }

  get loading(): boolean {
    return this.#loading;
  }

  /** Why the parts could not be read. */
  get fault(): unknown {
    return this.#fault;
  }

  /** Reads the parts of one folder. */
  async readFolder(query: FolderQuery): Promise<void> {
    await this.#read(async (signal) => {
      const listing = await this.#parts.list(
        {
          folder: query.folder,
          deep: query.deep,
          order: query.order,
          limit: query.limit,
          ...(query.tag === '' ? {} : { tag: query.tag }),
        },
        signal,
      );
      if (signal.aborted) return;
      this.#rows = listing.parts.map(cardPartOf);
      this.#matched = listing.matched;
      this.#missing = 0;
    });
  }

  /**
   * Reads the chosen parts, one request each, in the order they were chosen.
   * A part that is gone, or that the person may not see, is left out and
   * counted, so the card can say so.
   */
  async readChosen(ids: readonly number[]): Promise<void> {
    await this.#read(async (signal) => {
      const answers = await Promise.allSettled(
        ids.map((id) => this.#parts.part(id, signal)),
      );
      // A card that left, or a newer read, drops this one whole: an aborted
      // request is not a part that is gone.
      if (signal.aborted) return;
      const found = answers.flatMap((answer) =>
        answer.status === 'fulfilled' ? [cardPartOfDetail(answer.value)] : [],
      );
      // Every request failing is a fault, such as a module that is not
      // running. Some failing is parts that are gone.
      const failed = answers.find((answer) => answer.status === 'rejected');
      if (found.length === 0 && failed !== undefined && ids.length > 0) {
        throw failed.reason;
      }
      this.#rows = found;
      this.#matched = found.length;
      this.#missing = ids.length - found.length;
    });
  }

  /** Drops a read still on its way, for a card that leaves the page. */
  stop(): void {
    this.#abort?.abort();
    this.#abort = undefined;
  }

  async #read(run: (signal: AbortSignal) => Promise<void>): Promise<void> {
    this.#abort?.abort();
    const abort = new AbortController();
    this.#abort = abort;
    this.#loading = true;
    this.#fault = undefined;
    try {
      await run(abort.signal);
    } catch (thrown) {
      if (abort.signal.aborted) return;
      this.#fault = thrown;
    } finally {
      if (!abort.signal.aborted) this.#loading = false;
      if (this.#abort === abort) this.#abort = undefined;
    }
  }
}
