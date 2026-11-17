// The ports, what arrived and the sources it came from. One store for the
// page and the printer tab, so both read one answer (svelte-data.md).
import type { LogsApi, Source, Status, StreamStatus } from './api';

/** How often a page that shows the status reads it again, in milliseconds. */
export const STATUS_EVERY = 15_000;

/**
 * How long a stream counts as receiving after its newest datagram. A printer
 * that sends metrics does so every second, so two minutes of silence is a
 * printer that stopped.
 */
export const RECEIVING_FOR = 120_000;

/** What one stream is doing, as the page names it. */
export type StreamState = 'notGranted' | 'failed' | 'receiving' | 'waiting';

/** One stream's state, judged at `now`. */
export const streamState = (stream: StreamStatus, now: number): StreamState => {
  if (!stream.listener.granted) return 'notGranted';
  if (!stream.listener.listening) return 'failed';

  const last =
    stream.last_received === undefined ? NaN : Date.parse(stream.last_received);

  return now - last <= RECEIVING_FOR ? 'receiving' : 'waiting';
};

export class StatusStore {
  readonly #api: Pick<LogsApi, 'readStatus' | 'listSources' | 'assignSource'>;
  #status = $state.raw<Status | undefined>(undefined);
  #sources = $state.raw<readonly Source[] | undefined>(undefined);
  #fault = $state.raw<unknown>(undefined);
  #readAt = $state(0);
  #loading = $state(false);
  #flight: Promise<void> | undefined;
  #abort: AbortController | undefined;

  constructor(
    api: Pick<LogsApi, 'readStatus' | 'listSources' | 'assignSource'>,
  ) {
    this.#api = api;
  }

  /** Undefined until the first answer. */
  get status(): Status | undefined {
    return this.#status;
  }

  /** Every MAC that sent something; undefined until the first answer. */
  get sources(): readonly Source[] | undefined {
    return this.#sources;
  }

  /** What the last read was refused with. */
  get fault(): unknown {
    return this.#fault;
  }

  get loading(): boolean {
    return this.#loading;
  }

  /** When the last answer arrived, in milliseconds; 0 before the first. */
  get readAt(): number {
    return this.#readAt;
  }

  /**
   * Reads the status again every STATUS_EVERY until the answer stops it, so
   * a stream whose printer stopped sending stops showing as receiving.
   */
  watch(): () => void {
    const timer = setInterval(() => {
      void this.load();
    }, STATUS_EVERY);

    return () => {
      clearInterval(timer);
    };
  }

  /** The MACs no printer is matched to, for the printer filter. */
  get unmatched(): readonly Source[] {
    return (this.#sources ?? []).filter((one) => !one.device_id);
  }

  /** Reads both again. A read while one is in flight joins it. */
  load(): Promise<void> {
    if (this.#flight) return this.#flight;

    this.#abort = new AbortController();
    const signal = this.#abort.signal;
    this.#loading = true;
    this.#flight = Promise.all([
      this.#api.readStatus({}, signal),
      this.#api.listSources({}, signal),
    ])
      .then(([status, sources]) => {
        this.#status = status;
        this.#sources = sources.sources;
        this.#readAt = Date.now();
        this.#fault = undefined;
      })
      .catch((thrown: unknown) => {
        if (!signal.aborted) this.#fault = thrown;
      })
      .finally(() => {
        this.#loading = false;
        this.#flight = undefined;
      });

    return this.#flight;
  }

  /**
   * Matches a MAC to a printer by hand, or takes the match away with an empty
   * id, then reads the sources again. A refusal is thrown for the caller to
   * show beside the row.
   */
  async assign(mac: string, deviceId: string): Promise<void> {
    await this.#api.assignSource({
      path: { mac },
      body: { device_id: deviceId },
    });
    await this.load();
  }

  /** Stops a read in flight; called when the person leaves. */
  dispose(): void {
    this.#abort?.abort();
  }
}
