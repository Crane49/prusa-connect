// The log lines a view shows: the filters the person chose, the lines they
// match newest first, the way back to older ones, and the live mode that
// asks for newer ones every two seconds.
//
// One instance per view, because the page and a printer's tab filter apart.
// Every request it makes carries its own AbortController, so a change of
// filter cancels the answer it replaces and a stale page never lands. A
// cursor the module refuses because a purge or a restart moved the records
// starts the view again from the newest page.
import type { LogLine, LogsApi } from './api';
import { isStale } from './faults';
import {
  ALL,
  type FilterLevel,
  type Origin,
  originQuery,
  sinceOf,
  type Window,
} from './presentation';

/** How many lines one read asks for. */
export const PAGE = 200;

/** How often the live mode asks, in milliseconds. */
export const LIVE_EVERY = 2_000;

/** The most lines a view keeps, so a live view left open stays small. */
export const KEEP = 2_000;

export interface LogFilter {
  origin: Origin;
  /** The least serious level still shown, or undefined for every line. */
  level: FilterLevel | undefined;
  component: string;
  text: string;
  window: Window;
}

export const DEFAULT_FILTER: LogFilter = {
  origin: ALL,
  level: undefined,
  component: '',
  text: '',
  window: 24,
};

type Query = NonNullable<Parameters<LogsApi['listLogs']>[0]>['query'];

export class LogsStore {
  readonly #api: Pick<LogsApi, 'listLogs'>;
  readonly #now: () => number;
  #filter = $state.raw<LogFilter>(DEFAULT_FILTER);
  #lines = $state.raw<readonly LogLine[] | undefined>(undefined);
  #older = $state<string | undefined>(undefined);
  #newest = $state<string | undefined>(undefined);
  #fault = $state.raw<unknown>(undefined);
  #loading = $state(false);
  #live = $state(false);
  #timer: ReturnType<typeof setInterval> | undefined;
  #abort: AbortController | undefined;
  #polling = false;

  constructor(
    api: Pick<LogsApi, 'listLogs'>,
    filter: Partial<LogFilter> = {},
    now: () => number = Date.now,
  ) {
    this.#api = api;
    this.#now = now;
    this.#filter = { ...DEFAULT_FILTER, ...filter };
  }

  get filter(): LogFilter {
    return this.#filter;
  }

  /** Undefined until the first answer, empty when nothing matched. */
  get lines(): readonly LogLine[] | undefined {
    return this.#lines;
  }

  /** Whether an older page is there to ask for. */
  get hasOlder(): boolean {
    return this.#older !== undefined;
  }

  get fault(): unknown {
    return this.#fault;
  }

  get loading(): boolean {
    return this.#loading;
  }

  get live(): boolean {
    return this.#live;
  }

  /** The query every request of this view starts from. */
  query(): Query {
    const f = this.#filter;
    const out: Query = {
      ...originQuery(f.origin),
      since: sinceOf(f.window, this.#now()),
    };
    if (f.level !== undefined) out.severity = f.level;
    if (f.component.trim() !== '') out.component = f.component.trim();
    if (f.text.trim() !== '') out.q = f.text.trim();

    return out;
  }

  /** Changes the filters and reads the newest page for them. */
  set(next: Partial<LogFilter>): Promise<void> {
    this.#filter = { ...this.#filter, ...next };

    return this.load();
  }

  /** Reads the newest page for the filters, replacing what is shown. */
  async load(): Promise<void> {
    const signal = this.#fresh();
    this.#loading = true;
    try {
      const page = await this.#api.listLogs(
        { query: { ...this.query(), limit: PAGE } },
        signal,
      );
      if (signal.aborted) return;
      this.#lines = page.lines;
      this.#older = page.older;
      this.#newest = page.newest;
      this.#fault = undefined;
    } catch (thrown) {
      if (!signal.aborted) this.#fault = thrown;
    } finally {
      if (!signal.aborted) this.#loading = false;
    }
  }

  /** Adds the next older page under what is shown. */
  async loadOlder(): Promise<void> {
    const before = this.#older;
    if (before === undefined || this.#loading) return;

    const signal = this.#fresh();
    let restart = false;
    this.#loading = true;
    try {
      const page = await this.#api.listLogs(
        { query: { ...this.query(), before, limit: PAGE } },
        signal,
      );
      if (signal.aborted) return;
      this.#lines = [...(this.#lines ?? []), ...page.lines];
      this.#older = page.older;
      this.#fault = undefined;
    } catch (thrown) {
      if (signal.aborted) return;
      if (isStale(thrown)) restart = true;
      else this.#fault = thrown;
    } finally {
      if (!signal.aborted) this.#loading = false;
    }
    if (restart) await this.load();
  }

  /** Switches the live mode on or off. */
  setLive(on: boolean): void {
    this.#live = on;
    if (this.#timer !== undefined) clearInterval(this.#timer);
    this.#timer = undefined;
    if (on) {
      this.#timer = setInterval(() => {
        void this.poll();
      }, LIVE_EVERY);
    }
  }

  /**
   * Asks for the lines that came after the newest one shown and puts them on
   * top. A poll still running is not overlapped by the next one.
   */
  async poll(): Promise<void> {
    const after = this.#newest;
    if (after === undefined || this.#polling || this.#loading) {
      if (after === undefined && !this.#loading) await this.load();

      return;
    }

    this.#polling = true;
    const signal = this.#abort?.signal;
    let restart = false;
    try {
      const query = this.query();
      // The window is where reading starts; a poll reads from the newest line.
      delete query?.since;
      const page = await this.#api.listLogs(
        { query: { ...query, after, limit: PAGE } },
        signal,
      );
      if (signal?.aborted || this.#newest !== after) return;
      if (page.lines.length > 0) {
        this.#lines = [...page.lines, ...(this.#lines ?? [])].slice(0, KEEP);
      }
      this.#newest = page.newest ?? after;
      this.#fault = undefined;
    } catch (thrown) {
      if (signal?.aborted) return;
      if (isStale(thrown)) restart = true;
      else this.#fault = thrown;
    } finally {
      this.#polling = false;
    }
    if (restart) await this.load();
  }

  /** Stops the live mode and every request; called when the view goes. */
  dispose(): void {
    this.setLive(false);
    this.#abort?.abort();
  }

  #fresh(): AbortSignal {
    this.#abort?.abort();
    this.#abort = new AbortController();

    return this.#abort.signal;
  }
}
