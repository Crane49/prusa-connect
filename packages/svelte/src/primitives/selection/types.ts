import type { Selection } from './selection.svelte';

export interface SelectionOptions<Key> {
  /**
   * Every row the person may tick, on every page, in the order the listing
   * draws them. A shift range and "select all" both read this order.
   */
  rows: () => readonly Key[];
  /**
   * Told the ticked rows after each change the person makes, for a page that
   * keeps its own copy. A row leaving the listing is not such a change.
   */
  onchange?: ((keys: readonly Key[]) => void) | undefined;
}

export type SelectionState = 'none' | 'some' | 'all';

export interface SelectCheckboxProps<Key> {
  selection: Selection<Key>;
  key: Key;
  /** Names the row, such as "Select benchy.gcode". */
  label: string;
  /** Lets a `<label for>` or a focus call reach the box. */
  id?: string | undefined;
  disabled?: boolean | undefined;
  class?: string | undefined;
}

export interface SelectAllCheckboxProps<Key> {
  selection: Selection<Key>;
  /** The rows this box ticks: the page on screen, or one group. */
  keys: readonly Key[];
  label: string;
  disabled?: boolean | undefined;
  class?: string | undefined;
}
