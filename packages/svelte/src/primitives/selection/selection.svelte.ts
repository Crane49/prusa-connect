import type { SelectionOptions, SelectionState } from './types';

/**
 * The rows a person has ticked in one listing.
 *
 * A tick counts only while its row is among `rows()`. A row that left the
 * listing, after a delete or a refresh, is never acted on, and the screen
 * still clears the selection itself when the person walks to another folder
 * or searches.
 */
export class Selection<Key> {
  /** In the order the person ticked them. */
  #ticked = $state.raw<readonly Key[]>([]);
  /** The last row ticked or unticked on its own, where a shift range starts. */
  #anchor: Key | undefined;

  readonly #rows: () => readonly Key[];
  readonly #onchange: ((keys: readonly Key[]) => void) | undefined;
  /** Looked up once per row drawn, so a set rather than a scan. */
  readonly #listed: ReadonlySet<Key>;

  constructor(options: SelectionOptions<Key>) {
    this.#rows = options.rows;
    this.#onchange = options.onchange;
    // Rebuilt whole whenever the rows change and never written to.
    // eslint-disable-next-line svelte/prefer-svelte-reactivity
    this.#listed = $derived(new Set(this.#rows()));
  }

  /** The ticked rows still in the listing, in the order they were ticked. */
  get keys(): readonly Key[] {
    return this.#ticked.filter((key) => this.#listed.has(key));
  }

  get count(): number {
    return this.keys.length;
  }

  /** How many rows the listing offers, ticked or not. */
  get total(): number {
    return this.#rows().length;
  }

  has(key: Key): boolean {
    return this.#listed.has(key) && this.#ticked.includes(key);
  }

  /**
   * Ticks or unticks one row. With `range`, every row between the last one
   * pressed and this one, in the listing's order, takes this row's new state,
   * the way a mail client's shift-click does.
   */
  toggle(key: Key, range = false): void {
    const on = !this.has(key);
    const rows = this.#rows();
    const from = this.#anchor === undefined ? -1 : rows.indexOf(this.#anchor);
    const to = rows.indexOf(key);

    this.#anchor = key;

    if (!range || from === -1 || to === -1) {
      this.set([key], on);

      return;
    }

    this.set(rows.slice(Math.min(from, to), Math.max(from, to) + 1), on);
  }

  /** Ticks or unticks these rows together: a page, a group, one row. */
  set(keys: readonly Key[], on: boolean): void {
    const ticked = this.#ticked;

    this.#ticked = on
      ? [...ticked, ...keys.filter((key) => !ticked.includes(key))]
      : ticked.filter((key) => !keys.includes(key));
    this.#onchange?.(this.keys);
  }

  /** Every row in the listing, on every page. */
  selectAll(): void {
    this.set(this.#rows(), true);
  }

  clear(): void {
    this.#ticked = [];
    this.#anchor = undefined;
    this.#onchange?.([]);
  }

  /** Whether none, some or all of these rows are ticked: a header box. */
  stateOf(keys: readonly Key[]): SelectionState {
    const ticked = keys.filter((key) => this.has(key)).length;

    if (ticked === 0) return 'none';

    return ticked === keys.length ? 'all' : 'some';
  }
}
