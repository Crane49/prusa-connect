// The search a card's settings offer to choose parts with. A class, so the
// editor holds no request logic of its own (svelte-5.md).
//
// A search runs a quarter of a second after the person stops typing, and a
// newer one drops the read still in flight, so the answer on screen is always
// the one for the words in the box.
import type { PartsService } from '../parts';
import { type CardPart, cardPartOf } from './card-parts.svelte';

/** How long the search waits after the last key, in milliseconds. */
const QUIET_MS = 250;
/** How many parts one search answers. */
const SHOWN = 20;

export class PartSearch {
  readonly #parts: PartsService;
  #rows = $state.raw<readonly CardPart[]>([]);
  #loading = $state(false);
  #searched = $state(false);
  #fault = $state.raw<unknown>(undefined);
  #timer: ReturnType<typeof setTimeout> | undefined;
  #abort: AbortController | undefined;

  constructor(parts: PartsService) {
    this.#parts = parts;
  }

  get rows(): readonly CardPart[] {
    return this.#rows;
  }

  /** True from the first key until the answer for the words in the box lands. */
  get loading(): boolean {
    return this.#loading;
  }

  /** True once a search answered, so an empty answer can say it found nothing. */
  get searched(): boolean {
    return this.#searched;
  }

  get fault(): unknown {
    return this.#fault;
  }

  /** Searches for these words once the person stops typing. */
  search(q: string): void {
    clearTimeout(this.#timer);
    this.#loading = true;
    this.#timer = setTimeout(() => {
      void this.#read(q.trim());
    }, QUIET_MS);
  }

  /** Stops a search that is waiting or on its way. */
  stop(): void {
    clearTimeout(this.#timer);
    this.#abort?.abort();
    this.#loading = false;
  }

  async #read(q: string): Promise<void> {
    this.#abort?.abort();
    const abort = new AbortController();
    this.#abort = abort;
    this.#fault = undefined;

    try {
      const listing = await this.#parts.list(
        { order: 'changed', limit: SHOWN, ...(q === '' ? {} : { q }) },
        abort.signal,
      );
      if (abort.signal.aborted) return;
      this.#rows = listing.parts.map(cardPartOf);
      this.#searched = true;
    } catch (thrown) {
      if (abort.signal.aborted) return;
      this.#fault = thrown;
    } finally {
      if (this.#abort === abort) this.#loading = false;
    }
  }
}
