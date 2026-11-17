// One printer's readings as they arrive, for the device page and the state
// tree on the tools page.
import type { EventSubscription, StreamEvent } from '@prusa/connect';

import { type TelemetryPoint } from '../domain';
import { type DataFault, faultOf } from './fault';
import {
  sampleOf,
  TELEMETRY_EVENTS,
  type TelemetryClient,
  type TelemetryValue,
} from './services/telemetry';

export type { TelemetryPoint };

/** One row of the state tree: a path and its newest value. */
export interface TelemetryReading {
  readonly path: string;
  readonly value: TelemetryValue;
  readonly at: number;
}

/** How the store is built beyond its client. */
export interface LiveTelemetryOptions {
  /** How many points each path keeps, for a small chart. */
  readonly history?: number;
  /**
   * The person the store reads for, such as `user:ada`. The event stream is
   * reopened after a drop only while the cookie still belongs to them.
   */
  readonly principal?: string;
}

const DEFAULT_HISTORY = 120;

/**
 * When a frame says it happened. The envelope carries a CloudEvents `time`,
 * which is the origin's own clock and the clock the stored history is spelled
 * in. A frame carrying none, or carrying something unparseable, is stamped
 * here instead: a point in the wrong place is better than a reading dropped.
 */
const instantOf = (event: StreamEvent): number => {
  const time = event.envelope?.time;
  if (typeof time !== 'string') return Date.now();

  const parsed = Date.parse(time);

  return Number.isNaN(parsed) ? Date.now() : parsed;
};

/**
 * A shared store, so a class (svelte-5.md). The client comes in through the
 * constructor, which lets a test pass a fake.
 *
 * One store per printer. The fleet store subscribes to six event types and
 * telemetry is not one of them: a fleet of four hundred printers reports
 * several times a second, and the fleet list draws none of it. This store
 * subscribes for one printer, so the cost is one subscription per open page.
 *
 * The map and each history are `$state.raw` and are replaced whole, so a frame
 * is one recomputation for the readers of that map and not one per path
 * (svelte-data.md).
 */
export class LiveTelemetry {
  readonly #client: TelemetryClient;
  readonly #deviceId: string;
  readonly #history: number;
  readonly #principal: string | undefined;

  #values = $state.raw<Readonly<Record<string, TelemetryReading>>>({});
  #series = $state.raw<Readonly<Record<string, readonly TelemetryPoint[]>>>({});
  #error = $state<DataFault | undefined>(undefined);
  #frames = $state(0);
  #subscription: EventSubscription | undefined;

  constructor(
    client: TelemetryClient,
    deviceId: string,
    options: LiveTelemetryOptions = {},
  ) {
    this.#client = client;
    this.#deviceId = deviceId;
    this.#history = options.history ?? DEFAULT_HISTORY;
    this.#principal = options.principal;
  }

  /** The printer this store is about. Read-only by design. */
  get deviceId(): string {
    return this.#deviceId;
  }

  /** Every path this store has seen, with its newest value. */
  get values(): Readonly<Record<string, TelemetryReading>> {
    return this.#values;
  }

  get error(): DataFault | undefined {
    return this.#error;
  }

  /** How many frames were applied. A page uses it to tell live from stalled. */
  get frames(): number {
    return this.#frames;
  }

  /** How many points each path keeps. */
  get history(): number {
    return this.#history;
  }

  /** The state tree as a sorted list, which is the order the table draws. */
  readonly rows: readonly TelemetryReading[] = $derived(
    Object.values(this.#values).sort((left, right) =>
      left.path.localeCompare(right.path),
    ),
  );

  /** Every path seen so far, sorted. */
  readonly paths: readonly string[] = $derived(
    this.rows.map((row) => row.path),
  );

  /** The newest value at one path, or undefined for a path never seen. */
  value(path: string): TelemetryValue | undefined {
    return this.#values[path]?.value;
  }

  /** The recent numeric points at one path, oldest first. */
  points(path: string): readonly TelemetryPoint[] {
    return this.#series[path] ?? [];
  }

  /**
   * Opens the stream for this printer's readings. The returned function
   * closes it, and the page calls it on teardown (svelte-testing.md).
   */
  listen(): () => void {
    this.#subscription?.close();
    const subscription = this.#client.events({
      types: TELEMETRY_EVENTS,
      origins: [`device:${this.#deviceId}`],
      ...(this.#principal === undefined ? {} : { principal: this.#principal }),
    });
    this.#subscription = subscription;
    void this.#consume(subscription);

    return () => {
      subscription.close();
      if (this.#subscription === subscription) this.#subscription = undefined;
    };
  }

  /**
   * Applies one frame. It is public so a test drives the store without a
   * socket, and so a page that already has a subscription can feed it.
   * A frame about another printer is ignored.
   */
  apply(event: StreamEvent): void {
    const sample = sampleOf(event);
    if (sample?.deviceId !== this.#deviceId) return;

    const at = instantOf(event);
    this.#values = {
      ...this.#values,
      [sample.path]: { path: sample.path, value: sample.value, at },
    };
    this.#frames += 1;

    if (typeof sample.value !== 'number') return;

    const kept = [
      ...(this.#series[sample.path] ?? []),
      { at, value: sample.value },
    ];
    this.#series = {
      ...this.#series,
      [sample.path]:
        kept.length > this.#history ? kept.slice(-this.#history) : kept,
    };
  }

  /** Empties the map and every history, for a page moving to another printer. */
  clear(): void {
    this.#values = {};
    this.#series = {};
    this.#frames = 0;
  }

  async #consume(subscription: EventSubscription): Promise<void> {
    try {
      for await (const event of subscription) {
        this.apply(event);
      }
    } catch (thrown) {
      // The server ended the stream: the credential expired, or it refused at
      // reconnect. Both are for a person to see rather than for a retry loop.
      this.#error = faultOf(thrown);
    }
  }
}

/** One store per printer, built where the application builds its `PrusaConnect`. */
export const createLiveTelemetry = (
  client: TelemetryClient,
  deviceId: string,
  options?: LiveTelemetryOptions,
): LiveTelemetry => new LiveTelemetry(client, deviceId, options);
