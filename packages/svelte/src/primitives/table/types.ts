import type {
  ClassValue,
  HTMLAttributes,
  HTMLTableAttributes,
  HTMLTdAttributes,
  HTMLThAttributes,
} from 'svelte/elements';

export type TableProps = HTMLTableAttributes;

export type TableHeaderProps = HTMLAttributes<HTMLTableSectionElement>;

export type TableBodyProps = HTMLAttributes<HTMLTableSectionElement>;

export type TableFooterProps = HTMLAttributes<HTMLTableSectionElement>;

export type TableRowProps = HTMLAttributes<HTMLTableRowElement>;

/**
 * `scope` is `col` unless the caller says otherwise. A header cell that names
 * a row rather than a column passes `scope="row"`.
 */
export type TableHeadProps = HTMLThAttributes;

export type TableCellProps = HTMLTdAttributes;

/**
 * Svelte declares `<caption>` as `HTMLAttributes<HTMLElement>` and not as
 * `HTMLTableCaptionElement`, so the narrower element type cannot be spread
 * onto the tag.
 */
export type TableCaptionProps = HTMLAttributes<HTMLElement>;

export interface SortableTableHeadProps {
  label: string;
  /** `undefined` when this is not the sorted column. */
  direction: 'ascending' | 'descending' | undefined;
  onSort: () => void;
  align?: 'start' | 'end' | undefined;
  /** Visibility utilities for the `th`, for example `hidden @3xl/jobs:table-cell`. */
  class?: ClassValue | undefined;
}
