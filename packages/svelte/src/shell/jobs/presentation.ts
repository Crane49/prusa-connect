// The print as the listing reads it, and the narrowing a reader applies to it.
//
// The facets are counted here over this screen's two dimensions; the printers
// listing has the server count its own. The listing kit is to grow the
// reader's sort and search for everybody; until it has, both
// live here.
import { printerStateOf } from '../../domain';
import type { ProgressTone } from '../../primitives';
import { printerHref } from '../printers/presentation';
import type {
  JobFacetSelection,
  JobRecord,
  JobRow,
  JobSort,
  JobSortColumn,
  JobsPrinter,
  JobsSource,
} from './types';

const MILLISECONDS = 1000;

/** Newest first: a farm opens this list to see what just happened. */
export const DEFAULT_SORT: JobSort = { column: 'started', ascending: false };

export const NO_JOBS_SELECTED: JobFacetSelection = { state: [], printer: [] };

/**
 * How long the print ran. A print still on the bed has no span yet: `ended` is
 * what closes one, so the progress bar is what that row says instead.
 */
const spanOf = (record: JobRecord): number | undefined => {
  if (record.ended === undefined) return undefined;

  const seconds =
    (new Date(record.ended).getTime() - new Date(record.started).getTime()) /
    MILLISECONDS;

  return Number.isFinite(seconds) ? seconds : undefined;
};

/** Where one print's own page is. Its id is a UUID the core gives each print. */
export const jobHref = (id: string): string =>
  `/jobs/${encodeURIComponent(id)}`;

const rowOf = (printer: JobsPrinter, record: JobRecord): JobRow => ({
  id: record.id,
  fileName: record.display_name,
  path: record.path,
  // The machine's own word, read through the shared state vocabulary so a
  // print's badge and a printer's badge carry one colour scale.
  state: printerStateOf(record.state),
  printerId: printer.id,
  printerName: printer.name,
  progress: record.progress,
  started: record.started,
  durationSeconds: spanOf(record),
  running: record.ended === undefined,
  detailHref: jobHref(record.id),
  printerHref: printerHref(printer.id),
});

/** Every print this instance could read, flattened across the printers. */
export const jobRowsOf = (source: JobsSource): JobRow[] =>
  source.printers.flatMap((printer) =>
    (printer.jobs ?? []).map((record) => rowOf(printer, record)),
  );

const VALUE = {
  state: (row: JobRow): string => row.state,
  printer: (row: JobRow): string => row.printerId,
};

const DIMENSIONS = ['state', 'printer'] as const;

export type JobFacet = (typeof DIMENSIONS)[number];

/**
 * `except` names the dimension left unapplied, `null` applying both. The
 * search is never excepted: a chip counts what it would bring within what the
 * reader has already typed.
 */
const matchesExcept = (
  row: JobRow,
  selection: JobFacetSelection,
  query: string,
  except: JobFacet | null,
): boolean =>
  row.fileName.toLowerCase().includes(query) &&
  DIMENSIONS.every(
    (facet) =>
      facet === except ||
      selection[facet].length === 0 ||
      selection[facet].includes(VALUE[facet](row)),
  );

/** What a search box holds, as the filter reads it. */
export const searchTerm = (query: string): string => query.trim().toLowerCase();

export const filterJobs = (
  rows: readonly JobRow[],
  selection: JobFacetSelection,
  query: string,
): JobRow[] => rows.filter((row) => matchesExcept(row, selection, query, null));

/**
 * How many prints each value of `facet` would bring, with the other dimension
 * and the search already applied. A value present in `over` and matched by
 * nothing reads zero, which is a dead end the reader sees before pressing it.
 */
export const jobFacetCounts = (
  rows: readonly JobRow[],
  selection: JobFacetSelection,
  query: string,
  facet: JobFacet,
  over: readonly string[] = [],
): Map<string, number> => {
  const counts = new Map<string, number>(over.map((value) => [value, 0]));

  for (const row of rows) {
    if (!matchesExcept(row, selection, query, facet)) continue;
    const value = VALUE[facet](row);
    counts.set(value, (counts.get(value) ?? 0) + 1);
  }

  return counts;
};

/** The distinct values a dimension carries, in the order they first appear. */
export const jobFacetValues = (
  rows: readonly JobRow[],
  facet: JobFacet,
): string[] => Array.from(new Set(rows.map((row) => VALUE[facet](row))));

// A print still running has no span, and sorts as the shortest one.
const keyOf = (row: JobRow, column: JobSortColumn): number =>
  column === 'started'
    ? new Date(row.started).getTime()
    : (row.durationSeconds ?? 0);

export const compareJobs = (a: JobRow, b: JobRow, sort: JobSort): number => {
  const difference = keyOf(a, sort.column) - keyOf(b, sort.column);

  return sort.ascending ? difference : -difference;
};

/**
 * A second press on the same column reverses it; a press on the other one
 * starts descending, which is newest or longest first, the reading most visits
 * want without a third press.
 */
export const nextSort = (sort: JobSort, column: JobSortColumn): JobSort =>
  sort.column === column
    ? { column, ascending: !sort.ascending }
    : { column, ascending: false };

/**
 * What the filled part of the bar says. A print in motion is the interface's
 * own colour, one that finished reads as done, and one that stopped part way
 * is quiet: the row's badge is what says why it stopped.
 */
export const progressTone = (row: JobRow): ProgressTone => {
  if (row.running) return 'primary';

  return row.state === 'FINISHED' ? 'success' : 'muted';
};
