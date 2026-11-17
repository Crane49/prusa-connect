// What a printer's History tab reads, and when.
import { DeviceFacts, FACTS_PAGE_LIMIT } from './device-facts.svelte';
import { DeviceSamples, SAMPLES_PAGE_LIMIT } from './device-samples.svelte';
import {
  type HistoryClass,
  type HistoryClient,
  type HistoryWindow,
  instantOf,
} from './services/history';

const MS_PER_HOUR = 3_600_000;

/** These options set how the store behaves, beyond its client. */
export interface DeviceHistoryOptions {
  /** How far back the window reaches from the moment the tab opens. */
  readonly hours: number;
  /** The event classes the log reads. Omitted reads every class. */
  readonly classes?: readonly HistoryClass[];
  /** Read once when the tab opens, so the log can name each event type. */
  readonly eventTypes?: { read(): Promise<void> };
  /** The clock, which a test replaces. */
  readonly now?: () => number;
}

/**
 * One printer's stored past, read when its tab opens and not before. A shared
 * store, so a class (svelte-5.md).
 *
 * The window is fixed when the tab opens, so `Read more` pages within the
 * window the log already shows. The measurements are read only for the
 * metrics the chart draws, and read again when a reader turns on one that was
 * not asked for.
 */
export class DeviceHistory {
  readonly facts: DeviceFacts;
  readonly samples: DeviceSamples;
  readonly #options: DeviceHistoryOptions;

  #window = $state.raw<HistoryWindow | undefined>(undefined);
  #asked: readonly string[] = [];

  constructor(
    client: HistoryClient,
    deviceId: string,
    options: DeviceHistoryOptions,
  ) {
    this.facts = new DeviceFacts(client, deviceId);
    this.samples = new DeviceSamples(client, deviceId);
    this.#options = options;
  }

  /** The window the tab opened on, in milliseconds. Undefined until it opens. */
  readonly span: { readonly from: number; readonly to: number } | undefined =
    $derived(
      this.#window === undefined
        ? undefined
        : {
            from: Date.parse(this.#window.from),
            to: Date.parse(this.#window.to),
          },
    );

  /** Reads the window ending now: the events, and the measurements asked for. */
  async open(metrics: readonly string[]): Promise<void> {
    const to = (this.#options.now ?? Date.now)();
    const window: HistoryWindow = {
      from: instantOf(to - this.#options.hours * MS_PER_HOUR),
      to: instantOf(to),
      limit: FACTS_PAGE_LIMIT,
      ...(this.#options.classes === undefined
        ? {}
        : { classes: this.#options.classes }),
    };
    this.#window = window;
    this.#asked = [...metrics];

    await Promise.all([
      this.facts.load(window),
      this.#readSamples(window, metrics),
      this.#options.eventTypes?.read(),
    ]);
  }

  /**
   * Reads the measurements again where the chart now draws a metric not asked
   * for. The new read keeps every metric asked for before, so a line the
   * reader turns back on still has its points. An empty ask read every metric,
   * so nothing is missing from it.
   */
  async draw(metrics: readonly string[]): Promise<void> {
    const window = this.#window;
    if (window === undefined) return;
    if (this.#asked.length === 0) return;
    if (metrics.every((metric) => this.#asked.includes(metric))) return;

    const asked = this.#asked;
    this.#asked = [
      ...asked,
      ...metrics.filter((metric) => !asked.includes(metric)),
    ];
    await this.#readSamples(window, this.#asked);
  }

  /** Appends the next page of events, within the window the tab opened on. */
  async more(): Promise<void> {
    if (this.#window !== undefined) await this.facts.loadMore(this.#window);
  }

  /** Abandons both reads, for a tab that closed. */
  close(): void {
    this.facts.abort();
    this.samples.abort();
  }

  #readSamples(
    window: HistoryWindow,
    metrics: readonly string[],
  ): Promise<void> {
    this.samples.abort();

    return this.samples.load({
      from: window.from,
      to: window.to,
      limit: SAMPLES_PAGE_LIMIT,
      metrics,
    });
  }
}
