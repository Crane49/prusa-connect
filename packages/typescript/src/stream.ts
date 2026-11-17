import { ApiError, NetworkError } from "./errors.js";
import type { EventType } from "./generated/events.js";
import { operationTable } from "./generated/operations.js";
import type { Transport } from "./operation.js";
import { wait } from "./transport.js";

// The event stream, read over fetch. EventSource is not used: it sets no
// request header, so it cannot carry the bearer token, and it reads no HTTP
// status, so a refusal at connect looks like any other error (ADR-20260909-c7x rule 8).
//
// The wire is the HTML standard's text/event-stream: a frame is a run of
// lines ended by a blank line; a line is a field name, a colon and a value; a
// line starting with a colon is a comment. The server writes the CloudEvents
// envelope in `data`, the envelope's type in `event`, and the place to resume
// from in `id`. Two frames carry no data at all: `retry:` sets the
// reconnection delay, and a bare `id:` stamps the whole cursor after a window
// that moved the reader on more than one origin.

/**
 * The CloudEvents 1.0 envelope every frame of the stream carries in `data`.
 * The fields are the ones the core writes; `data` is the payload and stays
 * `unknown` until the core publishes a schema per type.
 */
export interface Envelope {
  readonly specversion: string;
  readonly id: string;
  readonly source: string;
  readonly type: string;
  readonly time: string;
  readonly subject?: string;
  readonly dataschema?: string;
  readonly datacontenttype?: string;
  // Zero-padded and scoped to `source`. Ordering is its job, never `time`'s.
  readonly sequence?: string;
  readonly traceparent?: string;
  readonly causationid?: string;
  // The numbering generation of `source`. It changes when the owning node's
  // store is recreated.
  readonly epoch?: string;
  readonly hops: number;
  readonly evclass: string;
  readonly ttl?: number;
  readonly authtype?: string;
  readonly authid?: string;
  readonly data?: unknown;
}

/**
 * One event the hub published. `type` is the envelope's type, narrowed to the
 * types the subscription asked for; `id` is this frame's place in the log,
 * absent on telemetry, which is conflated rather than replayed; `payload` is
 * the envelope's `data`; `envelope` is the whole envelope as the server sent
 * it.
 */
export interface HubEvent<T extends EventType = EventType> {
  readonly type: T;
  readonly id: string | undefined;
  readonly payload: unknown;
  readonly envelope: Envelope;
}

/**
 * The frame the server writes when this reader's place in the log is gone,
 * because it fell too far behind or resumed from a cursor no longer held.
 * The server writes it for one reader and there is no envelope behind it.
 * `id` is the cursor to carry on from, and the subscription resumes from it
 * on its own; `payload` is the server's `{cursor, dropped}`. Refetch state
 * from the API: there is no partial recovery.
 */
export interface ContinuityLostEvent {
  readonly type: ContinuityLost;
  readonly id: string | undefined;
  readonly payload: unknown;
  readonly envelope: undefined;
}

/**
 * The frame the server writes when it withdraws an origin from the reader's
 * sight: what the reader held of it is gone, and nothing more is coming from it. It names
 * the origin in `payload` and carries no envelope. `id` is the batch cursor.
 * It reaches a reader that asked for its type and for that origin, or for any.
 */
export interface OriginWithdrawnEvent {
  readonly type: OriginWithdrawn;
  readonly id: string | undefined;
  readonly payload: { readonly origin: string };
  readonly envelope: undefined;
}

/**
 * What a subscription yields: a hub event with its envelope, or a control
 * frame without one. Branch on `type`.
 */
export type StreamEvent<T extends EventType = EventType> =
  | HubEvent<Exclude<T, ContinuityLost | OriginWithdrawn>>
  | ContinuityLostEvent
  | OriginWithdrawnEvent;

/**
 * What a subscription asks for. `types` and `origins` narrow what the server
 * sends and go to the query as it declares them; `since` is a cursor from an
 * earlier event's `id` or from `lastEventId`, sent as `Last-Event-ID` on the
 * first connection; `signal` ends the subscription the same way `close()`
 * does. `reconnectDelay` is the wait before a reconnection when the server
 * has not sent a `retry:` of its own, in milliseconds, and `reconnectCeiling`
 * is the most the wait grows to.
 */
export interface EventsOptions<T extends EventType = EventType> {
  readonly types?: readonly T[];
  readonly origins?: readonly string[];
  // The coalescing window in milliseconds, between 100 and 10000.
  readonly interval?: number;
  readonly since?: string;
  readonly signal?: AbortSignal;
  readonly reconnectDelay?: number;
  readonly reconnectCeiling?: number;
  /**
   * The subject the stream is opened for, such as `user:ada`. When it is set,
   * the subscription asks `GET /api/v1/principal` before each reconnection.
   * A browser's cookie can belong to another person by then. The iteration
   * then ends with the same 401 `ApiError` as a credential that stopped
   * being accepted, and the stream is not opened for the other person.
   */
  readonly principal?: string;
}

/**
 * A subscription is an async iterable of events and a `close()`. The
 * iteration ends in one of two ways, and a consumer tells them apart by
 * whether the loop threw:
 *
 * - it returns, with no error, when the consumer ended it: `close()` was
 *   called, the `signal` aborted, or the loop was left with `break`
 * - it throws an `ApiError` when the server ended it: an
 *   `prusa-connect.auth.expired` frame, which is the credential no
 *   longer being accepted and arrives with `status` 401 and no `code`, or a
 *   refusal at connect or reconnect, which carries the server's status and
 *   `code`. Mint or log in again rather than reconnecting on the same token
 *
 * A connection that dropped is neither: the subscription reconnects on its
 * own with `Last-Event-ID` set to `lastEventId`, so the consumer sees the
 * events it missed and never the gap. A 502, 503 or 504 at connect or
 * reconnect is a drop too, because a reverse proxy answers with one of them
 * while the core behind it is down. A reader that fell too far behind is
 * told so by a `com.prusa.connect.stream.continuity.lost` event, whose `id`
 * is the cursor to carry on from; refetch state from the API on it.
 *
 * `lastEventId` is the cursor the next reconnection resumes from. Keep it
 * across a process restart and pass it as `since`.
 */
export interface EventSubscription<T extends EventType = EventType> extends AsyncIterable<StreamEvent<T>> {
  readonly lastEventId: string | undefined;
  close(): void;
}

// The type of the frame the server writes just before it closes a stream
// whose credential stopped being accepted. It is not a hub event and is not in
// the generated union: it belongs to one connection.
const CREDENTIAL_EXPIRED = "prusa-connect.auth.expired";

// The one hub event the server writes on its own, past any types filter, so a
// narrowed subscription still carries it.
const CONTINUITY_LOST = "com.prusa.connect.stream.continuity.lost" satisfies EventType;

/** The event every subscription may yield whatever `types` it asked for. */
export type ContinuityLost = typeof CONTINUITY_LOST;

const ORIGIN_WITHDRAWN = "com.prusa.connect.stream.origin.withdrawn" satisfies EventType;

/** The event that says an origin was withdrawn; it follows `types` like any other. */
export type OriginWithdrawn = typeof ORIGIN_WITHDRAWN;

const isWithdrawnPayload = (value: unknown): value is { origin: string } =>
  value !== null && typeof value === "object" && typeof (value as { origin?: unknown }).origin === "string";

// The server's own default retry, so a reader it never told waits as long as
// one it did.
const DEFAULT_RECONNECT_DELAY = 2000;
const DEFAULT_RECONNECT_CEILING = 30_000;

/**
 * subscribe opens the stream over the transport and reads it until closed.
 * PrusaConnect.events is the way in; this is what it calls.
 */
export const subscribe = <T extends EventType>(
  transport: Transport,
  options: EventsOptions<T>,
): EventSubscription<T | ContinuityLost> => new Subscription<T | ContinuityLost>(transport, options);

class Subscription<T extends EventType> implements EventSubscription<T> {
  lastEventId: string | undefined;

  private readonly transport: Transport;
  private readonly query: Record<string, unknown>;
  private readonly reconnectDelay: number;
  private readonly reconnectCeiling: number;
  private readonly principal: string | undefined;
  private readonly controller = new AbortController();
  // The server's retry: hint, once it has sent one. It replaces reconnectDelay.
  private retry: number | undefined;
  private reader: { cancel(): Promise<void> } | undefined;
  private closed = false;

  constructor(transport: Transport, options: EventsOptions<EventType>) {
    this.transport = transport;
    this.lastEventId = options.since;
    this.reconnectDelay = options.reconnectDelay ?? DEFAULT_RECONNECT_DELAY;
    this.reconnectCeiling = options.reconnectCeiling ?? DEFAULT_RECONNECT_CEILING;
    this.principal = options.principal;

    const query: Record<string, unknown> = {};
    if (options.types !== undefined && options.types.length > 0) query["types"] = options.types;
    if (options.origins !== undefined && options.origins.length > 0) query["origins"] = options.origins;
    if (options.interval !== undefined) query["interval_ms"] = options.interval;
    this.query = query;

    const signal = options.signal;
    if (signal !== undefined) {
      if (signal.aborted) this.close();
      else signal.addEventListener("abort", () => this.close(), { once: true });
    }
  }

  close(): void {
    if (this.closed) return;
    this.closed = true;
    this.controller.abort();
    // A stub's body is not tied to the signal the way fetch's is, and a real
    // one is released sooner this way. A read in flight resolves as done.
    void this.reader?.cancel().catch(() => undefined);
  }

  async *[Symbol.asyncIterator](): AsyncGenerator<StreamEvent<T>, void, undefined> {
    // How many connections in a row ended without delivering a frame. The
    // wait doubles with it and returns to the base on a connection that opened.
    let failures = 0;
    let first = true;

    try {
      while (!this.closed) {
        let response: Response;
        try {
          if (!first) await this.checkPrincipal();
          first = false;
          response = await this.connect();
        } catch (err) {
          if (this.closed) return;
          if (!isDrop(err)) throw err;
          failures += 1;
          if (!(await this.pause(failures))) return;
          continue;
        }

        failures = 0;
        yield* this.read(response);
        if (this.closed) return;
        // The connection ended, cleanly or not, and either way the place to
        // carry on from is lastEventId.
        failures = 1;
        if (!(await this.pause(failures))) return;
      }
    } finally {
      this.close();
    }
  }

  // checkPrincipal throws when the credential no longer belongs to the
  // subject the stream was opened for. A refusal with 401 means nobody is
  // signed in, which ends the stream the same way.
  private async checkPrincipal(): Promise<void> {
    if (this.principal === undefined) return;

    let now: string | undefined;
    try {
      const answer = await this.transport.call<{ principal?: unknown }>(
        operationTable["get-principal"],
        {},
        this.controller.signal,
      );
      now = typeof answer.principal === "string" ? answer.principal : undefined;
    } catch (err) {
      if (!(err instanceof ApiError) || err.status !== 401) throw err;
    }

    if (now !== this.principal) {
      throw new ApiError(401, undefined, "the stream ended: the credential that opened it now belongs to another subject");
    }
  }

  private connect(): Promise<Response> {
    const header: Record<string, string> = { Accept: "text/event-stream" };
    if (this.lastEventId !== undefined) header["Last-Event-ID"] = this.lastEventId;
    return this.transport.call<Response>(
      operationTable["stream-events"],
      { query: this.query, header },
      this.controller.signal,
    );
  }

  // read yields the frames of one connection until it ends, whether the
  // server closed it or the socket failed under it. Both are a drop to the
  // caller, and the ApiError a goodbye frame raises passes through.
  private async *read(response: Response): AsyncGenerator<StreamEvent<T>, void, undefined> {
    const body = response.body;
    if (body === null) return;

    const reader = body.getReader();
    this.reader = reader;
    const parser = new Parser();
    const decoder = new TextDecoder();

    try {
      for (;;) {
        let chunk: Awaited<ReturnType<typeof reader.read>>;
        try {
          chunk = await reader.read();
        } catch {
          return;
        }
        if (chunk.done) break;

        const frames = parser.feed(decoder.decode(chunk.value, { stream: true }));
        for (const [at, frame] of frames.entries()) {
          const event = this.event(frame);
          // The cursor moves with the event the consumer is handed, and not
          // with the parser, which may be frames ahead inside one chunk. The
          // last event of a chunk takes the parser's place: the server closes
          // a window with a frame that holds only the cursor of every origin,
          // and a reader that resumes from this event's own id resumes one.
          this.lastEventId = (at === frames.length - 1 ? parser.lastEventId : frame.cursor) ?? this.lastEventId;
          yield event;
          if (this.closed) return;
        }
        this.lastEventId = parser.lastEventId ?? this.lastEventId;
      }
      parser.feed(decoder.decode());
      this.lastEventId = parser.lastEventId ?? this.lastEventId;
    } finally {
      this.reader = undefined;
      if (parser.retry !== undefined) this.retry = parser.retry;
      void reader.cancel().catch(() => undefined);
    }
  }

  // event turns a dispatched frame into what the consumer sees, or throws the
  // ApiError that ends the iteration when the frame is the server's goodbye.
  private event(frame: Frame): StreamEvent<T> {
    if (frame.event === CREDENTIAL_EXPIRED) {
      throw new ApiError(401, undefined, "the stream ended: the credential that opened it is no longer accepted");
    }

    let decoded: unknown;
    try {
      decoded = JSON.parse(frame.data);
    } catch (cause) {
      throw new TypeError(`the ${frame.event ?? "message"} frame does not carry JSON`, { cause });
    }

    if (frame.event === CONTINUITY_LOST) {
      return { type: CONTINUITY_LOST, id: frame.id, payload: decoded, envelope: undefined };
    }

    if (frame.event === ORIGIN_WITHDRAWN) {
      if (!isWithdrawnPayload(decoded)) {
        throw new TypeError(`the ${ORIGIN_WITHDRAWN} frame does not name an origin`);
      }
      return { type: ORIGIN_WITHDRAWN, id: frame.id, payload: decoded, envelope: undefined };
    }

    if (!isEnvelope(decoded)) {
      throw new TypeError(`the ${frame.event ?? "message"} frame does not carry an envelope`);
    }

    return {
      type: (frame.event ?? decoded.type) as Exclude<T, ContinuityLost | OriginWithdrawn>,
      id: frame.id,
      payload: decoded.data,
      envelope: decoded,
    };
  }

  // pause waits out the backoff before a reconnection. It answers false when
  // the subscription was closed while waiting.
  private async pause(failures: number): Promise<boolean> {
    const base = this.retry ?? this.reconnectDelay;
    const grown = Math.min(base * 2 ** (failures - 1), this.reconnectCeiling);
    // Equal jitter: between half the delay and the whole of it, so a fleet of
    // readers dropped together does not come back together.
    const delay = grown / 2 + Math.random() * (grown / 2);
    try {
      await wait(delay, this.controller.signal);
    } catch {
      return false;
    }
    return !this.closed;
  }
}

// The statuses a proxy or gateway answers while the core behind it is down.
// The core never refused anything, so they count as a drop, the same as no
// answer at all. A direct connection to a stopped core gets no answer, and a
// connection through a reverse proxy gets one of these.
const GATEWAY_STATUSES: ReadonlySet<number> = new Set([502, 503, 504]);

// isDrop tells a connection that could not reach the core from a refusal by
// the core. The first is reconnected and the second ends the iteration.
const isDrop = (err: unknown): boolean =>
  err instanceof NetworkError || (err instanceof ApiError && GATEWAY_STATUSES.has(err.status));

const isEnvelope = (value: unknown): value is Envelope =>
  value !== null &&
  typeof value === "object" &&
  typeof (value as { type?: unknown }).type === "string" &&
  typeof (value as { id?: unknown }).id === "string";

// A frame the parser dispatched: the event name, the data lines joined by a
// newline, the id this frame carried if it carried one, and the last id the
// stream had stamped when the frame was dispatched.
interface Frame {
  readonly event: string | undefined;
  readonly data: string;
  readonly id: string | undefined;
  readonly cursor: string | undefined;
}

// Parser is the HTML standard's event stream interpretation, fed text in
// whatever pieces the socket delivered. Lines end in CRLF, LF or CR. A CR that
// is the last character of a piece ends its line there, and an LF that opens
// the next piece is the rest of the same terminator. A frame the stream ended
// in the middle of, before its blank line, is never dispatched.
class Parser {
  // The last event id the stream stamped, as the standard keeps it: set at
  // dispatch, including the dispatch of a frame with no data.
  lastEventId: string | undefined;
  // The reconnection delay in milliseconds, once a retry: line has set one.
  retry: number | undefined;

  private buffer = "";
  private skipLF = false;
  private event: string | undefined;
  private data: string[] = [];
  private id: string | undefined;
  private idBuffer: string | undefined;

  feed(text: string): Frame[] {
    const frames: Frame[] = [];
    if (this.skipLF) {
      this.skipLF = false;
      if (text.startsWith("\n")) text = text.slice(1);
    }
    const buffer = this.buffer + text;
    let start = 0;

    for (;;) {
      const lf = buffer.indexOf("\n", start);
      const cr = buffer.indexOf("\r", start);
      let end: number;
      let next: number;

      if (cr === -1 && lf === -1) break;
      if (cr !== -1 && (lf === -1 || cr < lf)) {
        end = cr;
        if (cr === buffer.length - 1) {
          this.skipLF = true;
          next = cr + 1;
        } else {
          next = buffer[cr + 1] === "\n" ? cr + 2 : cr + 1;
        }
      } else {
        end = lf;
        next = lf + 1;
      }

      this.line(buffer.slice(start, end), frames);
      start = next;
    }

    this.buffer = buffer.slice(start);
    return frames;
  }

  private line(line: string, frames: Frame[]): void {
    if (line === "") {
      this.dispatch(frames);
      return;
    }
    if (line.startsWith(":")) return;

    const colon = line.indexOf(":");
    const field = colon === -1 ? line : line.slice(0, colon);
    let value = colon === -1 ? "" : line.slice(colon + 1);
    if (value.startsWith(" ")) value = value.slice(1);

    switch (field) {
      case "event":
        this.event = value;
        break;
      case "data":
        this.data.push(value);
        break;
      case "id":
        if (!value.includes("\0")) {
          this.idBuffer = value;
          this.id = value;
        }
        break;
      case "retry":
        if (/^\d+$/.test(value)) this.retry = Number(value);
        break;
      default:
        // A field the standard does not name is ignored.
        break;
    }
  }

  private dispatch(frames: Frame[]): void {
    if (this.idBuffer !== undefined) this.lastEventId = this.idBuffer;

    if (this.data.length > 0) {
      frames.push({ event: this.event, data: this.data.join("\n"), id: this.id, cursor: this.lastEventId });
    }

    this.event = undefined;
    this.data = [];
    this.id = undefined;
  }
}
