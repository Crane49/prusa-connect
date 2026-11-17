import type { Snippet } from 'svelte';

export interface BulkBarProps {
  /** How many rows are ticked. Zero draws the idle row instead. */
  count: number;
  /** Already-counted text, such as "3 selected". */
  label: string;
  clearLabel: string;
  onClear: () => void;
  /**
   * Offered while only some rows are ticked: ticks every row of the folder or
   * search, on every page. Already-counted text, such as "Select all 120".
   */
  selectAllLabel?: string | undefined;
  onSelectAll?: (() => void) | undefined;
  /** What a reader may do to the ticked rows. */
  actions?: Snippet | undefined;
  /** The row when nothing is ticked: a search box, a filter, a button. */
  idle?: Snippet | undefined;
}
