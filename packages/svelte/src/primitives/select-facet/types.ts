import type { Snippet } from 'svelte';

export interface SelectFacetOption {
  value: string;
  /**
   * Rich content is allowed, the way `FacetRow` allows it. A snippet carries
   * no words search can read, so pass `searchText` beside it.
   */
  label: string | Snippet;
  /** What search matches on. A plain string label is its own search text. */
  searchText?: string | undefined;
  count?: number | undefined;
  /** Unpickable, for a reason the caller states in `hint`. */
  disabled?: boolean | undefined;
  hint?: string | undefined;
}

export interface SelectFacetGroup {
  /** Distinct within one facet. It keys the each block and names the heading. */
  id: string;
  heading: string;
  options: SelectFacetOption[];
  /** Empty means no value picked in this dimension, which every caller reads as all. */
  selected: string[];
  onToggle: (value: string) => void;
}

export interface SelectFacetProps {
  /** One flat list. Mutually exclusive with `groups`. */
  options?: SelectFacetOption[] | undefined;
  selected?: string[] | undefined;
  onToggle?: ((value: string) => void) | undefined;
  /**
   * Two or more dimensions in one popover, each with its own heading, divider
   * and selection. For dimensions that answer the same question at different
   * resolutions, where splitting them across two triggers would make a reader
   * open both to see one answer.
   */
  groups?: SelectFacetGroup[] | undefined;
  /**
   * `multiple` toggles values and keeps the panel open; `single` replaces the
   * value and closes it.
   */
  mode?: 'single' | 'multiple' | undefined;
  /**
   * `single` only: the row that clears the filter. Picking it calls every
   * group's `onToggle` with an empty string, and the caller maps that to
   * whatever no filter is on its side.
   */
  allLabel?: string | undefined;
  /** `single` only: what the All row counts. Drawn only where the options carry counts. */
  allCount?: number | undefined;
  /** Worth turning on past eight options. Below that a search is furniture. */
  searchable?: boolean | undefined;
  searchPlaceholder?: string | undefined;
  searchAriaLabel?: string | undefined;
  /**
   * Makes the search box a create field as well: with a query that matches no
   * option's own text, one more row offers to make it. `searchable` only, the
   * query being what names the new value. The caller does the creating and
   * decides whether the new value is picked straight away.
   */
  onCreate?: ((name: string) => void) | undefined;
  /** The verb on that row, before the quoted query. */
  createLabel?: string | undefined;
}
