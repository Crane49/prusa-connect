// The metrics one printer sent: the names with their newest values, and one
// series drawn as a chart.
import type { MetricName, LogsApi, SeriesPoint } from './api';
import {
  ALL,
  type Origin,
  originQuery,
  sinceOf,
  type Window,
} from './presentation';

/** How many points a chart asks for. The module averages into that many. */
export const POINTS = 300;

export class MetricsStore {
  readonly #api: Pick<LogsApi, 'listMetricNames' | 'readMetricSeries'>;
  readonly #now: () => number;
  #origin = $state.raw<Origin>(ALL);
  #window = $state<Window>(1);
  #names = $state.raw<readonly MetricName[] | undefined>(undefined);
  #truncated = $state(false);
  #chosen = $state<string | undefined>(undefined);
  #field = $state('v');
  #points = $state.raw<readonly SeriesPoint[] | undefined>(undefined);
  #fault = $state.raw<unknown>(undefined);
  #loading = $state(false);
  #abort: AbortController | undefined;

  constructor(
    api: Pick<LogsApi, 'listMetricNames' | 'readMetricSeries'>,
    now: () => number = Date.now,
    start: {
      origin?: Origin;
      window?: Window;
      name?: string | undefined;
      field?: string | undefined;
    } = {},
  ) {
    this.#api = api;
    this.#now = now;
    this.#origin = start.origin ?? ALL;
    this.#window = start.window ?? 1;
    this.#chosen = start.name;
    this.#field = start.field ?? 'v';
  }

  get origin(): Origin {
    return this.#origin;
  }

  get window(): Window {
    return this.#window;
  }

  get names(): readonly MetricName[] | undefined {
    return this.#names;
  }

  /** Whether the module listed only some of the names or their fields. */
  get truncated(): boolean {
    return this.#truncated;
  }

  get chosen(): string | undefined {
    return this.#chosen;
  }

  get field(): string {
    return this.#field;
  }

  /** The fields of the chosen metric, so a custom metric can pick one. */
  get fields(): readonly string[] {
    return this.#names?.find((one) => one.name === this.#chosen)?.fields ?? [];
  }

  get points(): readonly SeriesPoint[] | undefined {
    return this.#points;
  }

  get fault(): unknown {
    return this.#fault;
  }

  get loading(): boolean {
    return this.#loading;
  }

  /** Reads the names for the printer and the window, then the chosen series. */
  async load(next: { origin?: Origin; window?: Window } = {}): Promise<void> {
    if (next.origin !== undefined) this.#origin = next.origin;
    if (next.window !== undefined) this.#window = next.window;

    const signal = this.#fresh();
    this.#loading = true;
    try {
      const answer = await this.#api.listMetricNames(
        { query: this.#query() },
        signal,
      );
      if (signal.aborted) return;
      this.#names = answer.names;
      this.#truncated = answer.truncated;
      this.#fault = undefined;
      const kept = answer.names.find((one) => one.name === this.#chosen);
      if (kept === undefined) {
        this.#chosen = answer.names[0]?.name;
        this.#field = answer.names[0]?.fields[0] ?? 'v';
      } else if (!kept.fields.includes(this.#field)) {
        this.#field = kept.fields[0] ?? 'v';
      }
      await this.#series(signal);
    } catch (thrown) {
      if (!signal.aborted) this.#fault = thrown;
    } finally {
      if (!signal.aborted) this.#loading = false;
    }
  }

  /** Draws another metric, or another field of the same one. */
  async choose(name: string, field?: string): Promise<void> {
    this.#chosen = name;
    const known = this.#names?.find((one) => one.name === name)?.fields ?? [];
    this.#field = field ?? known[0] ?? 'v';

    const signal = this.#fresh();
    this.#loading = true;
    try {
      await this.#series(signal);
    } catch (thrown) {
      if (!signal.aborted) this.#fault = thrown;
    } finally {
      if (!signal.aborted) this.#loading = false;
    }
  }

  dispose(): void {
    this.#abort?.abort();
  }

  async #series(signal: AbortSignal): Promise<void> {
    const name = this.#chosen;
    if (name === undefined) {
      this.#points = [];

      return;
    }
    const answer = await this.#api.readMetricSeries(
      {
        query: {
          ...this.#query(),
          name,
          field: this.#field,
          max_points: POINTS,
        },
      },
      signal,
    );
    if (!signal.aborted) this.#points = answer.points;
  }

  #query(): { device?: string; mac?: string; since: string } {
    return {
      ...originQuery(this.#origin),
      since: sinceOf(this.#window, this.#now()),
    };
  }

  #fresh(): AbortSignal {
    this.#abort?.abort();
    this.#abort = new AbortController();

    return this.#abort.signal;
  }
}
