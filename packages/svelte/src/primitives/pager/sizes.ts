/**
 * How many rows a page holds.
 *
 * 5 is a farm glancing at what is running now; 100 is someone auditing a
 * month of prints or reading a catalogue of thousands. Nothing between 5 and
 * 25 earns a step of its own: the gap is the point, since a person changing
 * this wants a different kind of view and not two more rows.
 */
export const PAGE_SIZES = [5, 25, 50, 100] as const;

export type PageSize = (typeof PAGE_SIZES)[number];

export const DEFAULT_PAGE_SIZE: PageSize = 25;
