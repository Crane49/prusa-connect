// What the page shows, as the query of its address says it: `?days=30`,
// `?printer=<id>`, `?tag=garage`. A person can send the link, and a
// printer's section opens the page on that printer. Plain functions over a
// string, so a test reads them without a browser.
import { type Days, daysOf } from '../data/stats.svelte';

export interface PageView {
  readonly days: Days;
  /** One printer by id, or empty for every printer. */
  readonly printer: string;
  /** One tag, or empty for every tag. */
  readonly tag: string;
}

export const WHOLE_PAGE: PageView = { days: 7, printer: '', tag: '' };

/** Longer than any id or tag the core keeps. */
const MAX_TEXT = 200;

export const pageViewOf = (search: string): PageView => {
  const query = new URLSearchParams(search);
  return {
    days: daysOf(query.get('days')),
    printer: (query.get('printer') ?? '').slice(0, MAX_TEXT),
    tag: (query.get('tag') ?? '').slice(0, MAX_TEXT),
  };
};

/** The view as a query string, with every default left out. */
export const searchOf = (view: PageView): string => {
  const query = new URLSearchParams();
  if (view.days !== 7) query.set('days', String(view.days));
  if (view.printer !== '') query.set('printer', view.printer);
  if (view.tag !== '') query.set('tag', view.tag);
  const written = query.toString();
  return written === '' ? '' : `?${written}`;
};
