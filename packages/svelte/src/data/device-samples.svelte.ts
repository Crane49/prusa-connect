// One printer's stored measurements over one window, grouped by metric.
//
// A window is read by following the cursor, and the follow is bounded. A
// Raspberry Pi serving a print must not be asked for an unbounded walk, so the
// walk stops at a budget and says so. A chart that drew part of a window and
// said nothing would be a chart that lies.
import type { TelemetryPoint } from '../domain';
import { type DataFault, faultOf } from './fault';
import {
  type HistoryClient,
  type HistorySample,
  type HistoryWindow,
  instantOf,
  readDeviceSamples,
} from './services/history';

/** How many rows one page asks for. */
export const SAMPLES_PAGE_LIMIT = 1000;

/** How many pages one window is allowed to cost. */
export const SAMPLES_MAX_PAGES = 8;

/**
 * The span of the first slice the walk reads, in milliseconds.
 *
 * The route answers a window from its oldest row forward and takes no order
 * of its own, so a single read of two hours spends the whole budget on the
 * two hours nobody opened this tab for. The walk instead reads a short slice
 * ending at `to`, then doubling slices backwards, so the budget is spent at
 * the recent end first and the points that meet the live stream are the ones
 * that survive.
 */
const FIRST_SLICE_MS = 300_000;

/**
 * The shortest slice the walk will narrow to before giving up on the rest of
 * the window. Two pages of a quarter-minute is a hundred and thirty readings
 * a second, which no printer reports.
 */
const MIN_SLICE_MS = 15_000;

/** How many pages one slice may cost before the walk tries a shorter one. */
const SLICE_PAGES = 2;

type Series = Readonly<Record<string, readonly TelemetryPoint[]>>;

const pointsOf = (
  rows: readonly HistorySample[],
): Record<string, TelemetryPoint[]> => {
  const grouped = Object.create(null) as Record<string, TelemetryPoint[]>;

  for (const row of rows) {
    // A path that reports a word answers `text` and no `value`. A word is not
    // a point on a line, so it is not this store's row.
    if (typeof row.value !== 'number') continue;

    const at = Date.parse(row.at);
    if (Number.isNaN(at)) continue;

    (grouped[row.metric] ??= []).push({ at, value: row.value });
  }

  return grouped;
};

const absorb = (
  into: Record<string, TelemetryPoint[]>,
  from: Record<string, TelemetryPoint[]>,
): void => {
  for (const [metric, points] of Object.entries(from)) {
    (into[metric] ??= []).push(...points);
  }
};

/** One printer's stored measurements. A shared store, so a class. */
export class DeviceSamples {
  readonly #client: HistoryClient;
  readonly #deviceId: string;

  #series = $state.raw<Series>({});
  #since = $state<number | undefined>(undefined);
  #truncated = $state(false);
  #incomplete = $state(false);
  #loading = $state(false);
  #loaded = $state(false);
  #error = $state<DataFault | undefined>(undefined);

  #flight: Promise<void> | undefined;
  #reading: AbortController | undefined;

  constructor(client: HistoryClient, deviceId: string) {
    this.#client = client;
    this.#deviceId = deviceId;
  }

  /** The points of each metric, oldest first. */
  get series(): Series {
    return this.#series;
  }

  /**
   * The oldest instant the walk reached, which is where a chart's time axis
   * starts. It is the window's own start where nothing was truncated.
   */
  get since(): number | undefined {
    return this.#since;
  }

  get loading(): boolean {
    return this.#loading;
  }

  get loaded(): boolean {
    return this.#loaded;
  }

  get error(): DataFault | undefined {
    return this.#error;
  }

  /** Whether this client gave up on the window at its own page budget. */
  get truncated(): boolean {
    return this.#truncated;
  }

  /** Whether the server itself reports a gap in what it kept for this window. */
  get incomplete(): boolean {
    return this.#incomplete;
  }

  /** Reads the window from its recent end back, up to the page budget. */
  load(window: HistoryWindow): Promise<void> {
    if (this.#flight) return this.#flight;

    this.abort();
    const reading = new AbortController();
    this.#reading = reading;
    this.#loading = true;

    // A flight a newer read replaced must not report that read finished.
    const flight = this.#walk(window, reading.signal).finally(() => {
      if (this.#flight !== flight) return;
      this.#flight = undefined;
      if (this.#reading === reading) this.#reading = undefined;
      this.#loading = false;
      this.#loaded = true;
    });
    this.#flight = flight;

    return flight;
  }

  /**
   * Abandons a read in flight, for a page moving to another printer.
   *
   * The flight is dropped with the controller. A caller that aborts and then
   * reads again in the same tick — which is what an effect's cleanup and its
   * rerun are — would otherwise be handed the promise that was just
   * abandoned, and its window would never be read.
   */
  abort(): void {
    this.#reading?.abort();
    this.#reading = undefined;
    this.#flight = undefined;
  }

  async #walk(window: HistoryWindow, signal: AbortSignal): Promise<void> {
    const start = Date.parse(window.from);
    const end = Date.parse(window.to);
    const grouped = Object.create(null) as Record<string, TelemetryPoint[]>;

    let edge = end;
    let span = FIRST_SLICE_MS;
    let pages = 0;
    let incomplete = false;

    try {
      while (edge > start && pages < SAMPLES_MAX_PAGES) {
        const from = Math.max(start, edge - span);
        // A slice is kept apart until it is whole. Half a slice would draw a
        // line from the window's oldest read row across a gap to the rows
        // above it, which is a line the printer never reported.
        const slice = Object.create(null) as Record<string, TelemetryPoint[]>;
        let cursor = '';
        let spent = 0;

        do {
          const answer = await readDeviceSamples(
            this.#client,
            this.#deviceId,
            {
              ...window,
              from: instantOf(from),
              to: instantOf(edge),
              cursor,
            },
            signal,
          );
          pages += 1;
          spent += 1;
          incomplete ||= answer.incomplete;
          absorb(slice, pointsOf(answer.items ?? []));
          cursor = answer.cursor;
        } while (
          cursor !== '' &&
          spent < SLICE_PAGES &&
          pages < SAMPLES_MAX_PAGES
        );

        if (cursor !== '') {
          // The slice is denser than the budget allows. A shorter one at the
          // same edge keeps the recent end rather than abandoning it.
          if (span > MIN_SLICE_MS) {
            span = Math.max(MIN_SLICE_MS, Math.floor(span / 4));
            continue;
          }

          break;
        }

        absorb(grouped, slice);
        edge = from;
        span *= 2;
      }

      const series: Record<string, readonly TelemetryPoint[]> = {};
      for (const [metric, points] of Object.entries(grouped)) {
        series[metric] = points.sort((one, other) => one.at - other.at);
      }

      this.#series = series;
      this.#since = edge;
      this.#truncated = edge > start;
      this.#incomplete = incomplete;
      this.#error = undefined;
    } catch (thrown) {
      if (signal.aborted) return;
      this.#error = faultOf(thrown);
      this.#series = {};
      this.#since = undefined;
      this.#truncated = false;
      this.#incomplete = false;
    }
  }
}
