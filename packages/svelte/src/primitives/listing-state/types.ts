import type { Snippet } from 'svelte';

import type { ListField } from '../field-picker/types';
import type { ViewModeOption } from '../view-mode/types';

/**
 * One filter, whatever it filters. Only the values and the words change between
 * listings, which is what lets one toolbar draw them all.
 */
export interface FacetDimension {
  id: string;
  label: string;
  values: string[];
  /** Absent where the server counts, or does not. */
  counts?: Map<string, number>;
  selected: readonly string[];
  onSelectedChange: (next: string[]) => void;
  /** Turns a raw value into the words a reader sees. */
  labelFor?: (value: string) => string;
  /** `single` replaces the pick and closes the panel, with an All row to clear. */
  mode?: 'multiple' | 'single';
  allLabel?: string;
  /** Overrides the rule that a long list gets a search box. */
  searchable?: boolean;
  /** Drawn in the popover instead of the value list, for a facet words do not carry. */
  panel?: Snippet;
  /** CSS colours drawn in the trigger. */
  swatches?: string[];
}

/**
 * Where a listing keeps what a reader chose. Narrow on purpose: a host hands in
 * `localStorage`, a test hands in a map, and neither is assumed to work.
 */
export interface ListingStore {
  read: (key: string) => string | null;
  write: (key: string, value: string) => void;
}

export interface ListingStateOptions<Row> {
  /** Names the storage key, so two listings do not share a preference. */
  id: string;
  /**
   * Which `DEFAULT_VIEW_FIELDS` entry decides this listing's opening shape.
   * Defaults to `id`; two listings of the same things name the same setting.
   */
  viewSetting?: string | undefined;
  fields: ListField[];
  views: ViewModeOption[];
  /** Where no setting names this listing. The reader's own choice outranks both. */
  initialView: string;
  rows: () => readonly Row[];
  store: ListingStore;
  /** Applied before paging. Omit to page the rows as they arrive. */
  compare?: (a: Row, b: Row) => number;
  /**
   * The server's count over every page. Given, the rows are one page as the
   * server cut it, and the listing neither orders nor slices them.
   */
  total?: () => number;
  /** `hero` drops the pager, the facets and the sort. */
  shape?: () => 'hero' | 'fleet';
}
