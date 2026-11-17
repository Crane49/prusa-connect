import type { EventType } from "./generated/events.js";
import { bindOperations, type Operations } from "./generated/operations.js";
import { browserPlatform, StreamSharing } from "./shared.js";
import { type ContinuityLost, type EventsOptions, type EventSubscription, subscribe } from "./stream.js";
import { combine, HttpTransport, type TransportOptions } from "./transport.js";

/**
 * What a PrusaConnect is built from: a credential, the core's origin when it
 * is not the page's own, the transport's tuning, and whether the tabs of a
 * browser share one event stream.
 */
export type PrusaConnectOptions = TransportOptions & {
  /**
   * When true, `events()` reads one stream for every subscription in every
   * tab of this browser that talks to the same core as the same person. One
   * tab reads it and passes each event to the others over a BroadcastChannel.
   * A browser opens at most six connections to one host over HTTP/1.1, and a
   * stream per tab spends one of them each. Without a BroadcastChannel, every
   * tab still reads one stream for all its own subscriptions.
   *
   * Each subscription then gets the events it asked for, as before. Two
   * things differ. An origin the caller may not observe is not refused: it
   * matches nothing. And the shared stream is opened with the shortest window
   * any subscription asked for.
   */
  readonly shareEvents?: boolean;
};

// The page's own sharing, made on the first subscription that shares, so a
// page that never shares never opens a channel.
let pageSharing: StreamSharing | undefined;

/**
 * PrusaConnect is one core, one credential, and every operation the core's
 * document declares as a method. The methods come from the generated
 * Operations interface; this class binds them to the transport. The
 * credential is a bearer token, or `{ kind: "session" }` from a page the core
 * serves, where the browser's own cookie is the credential.
 */
export class PrusaConnect {
  readonly url: string;
  readonly transport: HttpTransport;
  readonly #options: PrusaConnectOptions;
  // The transport a shared stream reads through. It has no signal: the stream
  // serves every client of the page, and a client's signal ends only that
  // client's own subscriptions.
  readonly #shared: HttpTransport | undefined;

  constructor(options: PrusaConnectOptions) {
    this.#options = options;
    this.transport = new HttpTransport(options);
    this.url = this.transport.url;
    if (options.shareEvents === true) {
      const { signal: _ended, ...unbound } = options;
      this.#shared = new HttpTransport(unbound);
    }
    Object.assign(this, bindOperations(this.transport));
  }

  /**
   * withSignal answers a client with the same credential and settings whose
   * every request ends when the signal aborts. A request in flight then is
   * aborted, and a request made after it is refused before it is sent. The
   * event streams it opened end too. This client is not ended with it.
   *
   * A host hands such a client to code it may stop, so that code cannot keep
   * acting once it was stopped.
   */
  withSignal(signal: AbortSignal): PrusaConnect {
    return new PrusaConnect({ ...this.#options, signal: combine(this.#options.signal, signal) ?? signal });
  }

  /**
   * events subscribes to `GET /api/v1/events` and reads it over fetch with
   * the same credential as every other call. Iterate the result with
   * `for await`; leave the loop, call `close()` or abort the `signal` to
   * stop. The subscription reconnects on its own after a drop and resumes
   * from the last id it saw. See EventSubscription for how an ending the
   * server chose is told from one the consumer chose. With `shareEvents`,
   * the stream is shared as that option says.
   */
  events<T extends EventType = EventType>(options: EventsOptions<T> = {}): EventSubscription<T | ContinuityLost> {
    if (this.#shared === undefined) return subscribe(this.transport, options);

    pageSharing ??= new StreamSharing(browserPlatform());
    return pageSharing.events(this.#shared, options, this.#options.signal);
  }
}

/**
 * The generated methods land on the instance in the constructor, and this
 * merge is what lets a caller see them on the type.
 */
export interface PrusaConnect extends Operations {}
