import type { ClassValue } from 'svelte/elements';

export interface ListingSearchProps {
  value: string;
  onValue: (next: string) => void;
  /** The box carries its own name: a label above it costs a row of chrome. */
  label: string;
  placeholder: string;
  /** The longest text the box takes; the server names it for a search it runs. */
  maxlength?: number | undefined;
  /**
   * When the box reports. `input` on every key; `change` once the text is
   * finished (Enter or leaving the box), for a search that asks the server.
   * Escape and the clear button report `''` either way, unless the box is
   * already empty.
   */
  commit?: 'input' | 'change';
  class?: ClassValue | undefined;
}

export interface ListingSearchSummaryProps {
  /** What the search matched, already written out by the caller. */
  summary: string;
  clearLabel: string;
  onClear: () => void;
  class?: ClassValue | undefined;
}
