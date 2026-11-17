import { ApiError, isProblem, type Problem } from "./errors.js";
import type { EventType } from "./generated/events.js";
import { operationTable } from "./generated/operations.js";
import type { Transport } from "./operation.js";
import { type ContinuityLost, type EventsOptions, type EventSubscription, type StreamEvent, subscribe } from "./stream.js";

// One event stream for every tab of a browser (ADR-20261023-a52).
//
// A browser opens at most six connections to one host over HTTP/1.1, which is
// what a server on a LAN with no TLS speaks. A stream holds its connection for
// as long as the page is open. Six tabs that each read their own stream leave
// no connection for any other request, and every tab then waits for ever.
//
// So one tab reads the stream and posts each event on a BroadcastChannel, and
// the other tabs listen there. The channel is named after the person signed
// in, and after the server when the client names one. A BroadcastChannel
// reaches only the pages of one origin, so a page's own server needs no name.
//
// The tabs choose the reading tab over the same channel. The Web Locks API
// would do it with less code, but a browser offers it only in a secure
// context. A server on a LAN address over plain HTTP is not one, and that is
// the server this is for. The choice works like this:
//
// - a tab that joins asks who reads, and the reading tab answers
// - with no answer, the tab claims the role. Of the tabs that claim it at the
//   same time, the one with the smallest id takes it
// - the reading tab says so when it stops or its page goes away, and the
//   others claim the role at once
// - each tab that listens asks again every few seconds. A reading tab that
//   stopped answering, because it crashed or was frozen, is replaced
// - two tabs that both read hear each other, and the one with the larger id
//   stops
//
// A tab that takes over opens the stream again from the last place it heard.
//
// Each subscription in each tab states what it wants: event types, origins
// and a window. The reading tab opens one stream for all of them. It asks for
// the union of the types, for every origin the person may see, and for the
// shortest window. Each subscription then drops what it did not ask for. When
// the union changes, the reading tab opens the stream again from where it
// stopped.
//
// Where the browser has no BroadcastChannel, every tab reads a stream of its
// own. That is still one stream for all the subscriptions of that tab.

/**
 * The server's own window when a reader names none. The union compares an
 * unnamed window with a named one, so it needs the number. It is
 * `DefaultInterval` in internal/transport/stream/hub.go.
 */
const SERVER_DEFAULT_INTERVAL = 200;

const CONTINUITY_LOST = "com.prusa.connect.stream.continuity.lost" satisfies EventType;
const ORIGIN_WITHDRAWN = "com.prusa.connect.stream.origin.withdrawn" satisfies EventType;

/** What one subscription asks the shared stream for. Origins are applied in the tab. */
interface Demand {
  readonly types: readonly string[] | undefined;
  readonly interval: number | undefined;
}

/** Why the stream ended, in a form a BroadcastChannel can carry. */
interface Failure {
  readonly status: number | undefined;
  readonly body: Problem | string | undefined;
  readonly message: string;
}

/** What the tabs post to each other. `from` is the posting tab. */
type Message =
  // Who reads? The reading tab answers with `lead`.
  | { readonly kind: "probe"; readonly from: string }
  // Nobody answered, and this tab offers to read.
  | { readonly kind: "claim"; readonly from: string }
  // This tab reads.
  | { readonly kind: "lead"; readonly from: string }
  // This tab stops reading, and the place the next reading tab resumes from.
  | { readonly kind: "resign"; readonly from: string; readonly cursor: string | undefined }
  // What a tab wants now. An empty list means nothing.
  | { readonly kind: "demand"; readonly from: string; readonly demands: readonly Demand[] }
  // The reading tab passes on one event and the place after it.
  | { readonly kind: "event"; readonly from: string; readonly event: StreamEvent; readonly cursor: string | undefined }
  // The server ended the stream, and every subscription ends with it.
  | { readonly kind: "ended"; readonly from: string; readonly failure: Failure };

/** The part of a BroadcastChannel this file uses, so a test can pass its own. */
export interface Channel {
  postMessage(message: Message): void;
  addEventListener(type: "message", listener: (event: MessageEvent) => void): void;
  close(): void;
}

/** How long each step of choosing the reading tab waits, in milliseconds. */
export interface Timing {
  /** How long a tab that asked who reads waits for an answer. */
  readonly answer: number;
  /** How long a tab that claimed the role waits for other claims. */
  readonly claim: number;
  /** How often a listening tab asks again whether the reading tab is there. */
  readonly check: number;
  /** How long the reading tab waits for the demands to settle before it opens the stream again. */
  readonly settle: number;
}

/**
 * The defaults. A reading tab answers within a few milliseconds, as the
 * answer is a message and no timer, so a tab waits a quarter of a second for
 * it. A tab that closes hands over in about that time, and one that crashed
 * is replaced within six seconds. A page that opens three subscriptions in
 * one render costs one stream and not three.
 */
const TIMING: Timing = { answer: 250, claim: 100, check: 5000, settle: 50 };

/**
 * What a tab offers to share a stream with the others. A browser without
 * BroadcastChannel offers nothing, and the tab reads alone.
 */
export interface Platform {
  readonly channel: ((name: string) => Channel) | undefined;
  /** Calls the listener when the page goes away, and answers the call that stops it. */
  readonly onPageHide: ((listener: () => void) => () => void) | undefined;
  readonly timing?: Partial<Timing>;
}

/** The browser's own BroadcastChannel, where it has one. */
export const browserPlatform = (): Platform => {
  // A browser before Safari 15.4 has none. The DOM types say it is always
  // there, so the read goes through an optional type.
  const scope = globalThis as {
    BroadcastChannel?: typeof BroadcastChannel;
    addEventListener?: typeof addEventListener;
    removeEventListener?: typeof removeEventListener;
  };
  const Broadcast = scope.BroadcastChannel;
  if (Broadcast === undefined) return { channel: undefined, onPageHide: undefined };

  const add = scope.addEventListener;
  const remove = scope.removeEventListener;

  return {
    channel: (name) => new Broadcast(name),
    onPageHide:
      add === undefined || remove === undefined
        ? undefined
        : (listener) => {
            add.call(globalThis, "pagehide", listener);
            return () => remove.call(globalThis, "pagehide", listener);
          },
  };
};

/**
 * The shared streams of one tab, one per server and person. A page has one;
 * a test builds one per tab it plays.
 */
export class StreamSharing {
  readonly #platform: Platform;
  readonly #streams = new Map<string, SharedStream>();

  constructor(platform: Platform) {
    this.#platform = platform;
  }

  /**
   * events is what PrusaConnect.events answers when sharing is on. `transport`
   * is the one the shared stream reads through, and `ended` is the client's
   * own signal, which ends this subscription and never the shared stream.
   */
  events<T extends EventType>(
    transport: Transport & { readonly url: string },
    options: EventsOptions<T>,
    ended: AbortSignal | undefined,
  ): EventSubscription<T | ContinuityLost> {
    const reader = new SharedReader<T | ContinuityLost>(options);

    // The client's signal outlives many subscriptions, so each listener goes
    // when its subscription ends.
    const close = (): void => reader.close();
    for (const signal of [options.signal, ended]) {
      if (signal === undefined) continue;
      if (signal.aborted) {
        reader.close();
        continue;
      }
      signal.addEventListener("abort", close, { once: true });
      reader.onClose(() => signal.removeEventListener("abort", close));
    }

    if (!reader.closed) void this.#attach(transport, options, reader);

    return reader;
  }

  // attach joins the reader to the stream of the person it is for. A reader
  // that names nobody is for whoever the cookie belongs to now, which the
  // server says. A stream is never shared on a guess about the person.
  async #attach(
    transport: Transport & { readonly url: string },
    options: EventsOptions<EventType>,
    reader: SharedReader<EventType>,
  ): Promise<void> {
    let principal = options.principal;
    if (principal === undefined) {
      try {
        principal = await currentPrincipal(transport, reader.signal);
      } catch (thrown) {
        reader.fail(failureOf(thrown));
        return;
      }
      if (principal === undefined) {
        reader.fail({ status: 401, body: undefined, message: "the stream was not opened: nobody is signed in" });
        return;
      }
    }
    if (reader.closed) return;

    const key = `${transport.url} ${principal}`;
    let stream = this.#streams.get(key);
    if (stream === undefined) {
      const created = new SharedStream(this.#platform, transport, key, principal, () => {
        if (this.#streams.get(key) === created) this.#streams.delete(key);
      });
      this.#streams.set(key, created);
      stream = created;
    }
    stream.join(reader, options.since);
  }
}

/**
 * One server and one person, in one tab. It reads the stream while this tab
 * is the reading tab, and listens to the reading tab otherwise.
 */
class SharedStream {
  readonly #tab = tabId();
  readonly #platform: Platform;
  readonly #timing: Timing;
  readonly #transport: Transport;
  readonly #name: string;
  readonly #principal: string;
  readonly #forget: () => void;
  readonly #readers = new Set<SharedReader<EventType>>();
  // The demands of the other tabs, kept by the reading tab alone.
  readonly #remote = new Map<string, readonly Demand[]>();
  #channel: Channel | undefined;
  #stopHide: (() => void) | undefined;
  // The tab that reads, this one included. Undefined while nobody is known to.
  #leader: string | undefined;
  // The tabs that claimed the role in the claim now running, this one
  // included. Undefined when no claim is running.
  #claims: Set<string> | undefined;
  // The one timer of the choice: the wait for an answer, the end of a claim
  // or the next check. A new step replaces it.
  #timer: ReturnType<typeof setTimeout> | undefined;
  // Whether the reading tab answered since this tab last asked.
  #answered = false;
  // The place after the last event this tab saw, which is where a stream this
  // tab opens resumes.
  #cursor: string | undefined;
  #stream: EventSubscription | undefined;
  // The union the open stream was asked for.
  #opened: Demand | undefined;
  #settle: ReturnType<typeof setTimeout> | undefined;

  constructor(platform: Platform, transport: Transport, key: string, principal: string, forget: () => void) {
    this.#platform = platform;
    this.#timing = { ...TIMING, ...platform.timing };
    this.#transport = transport;
    this.#name = `prusa-connect.events ${key}`;
    this.#principal = principal;
    this.#forget = forget;
  }

  get #leading(): boolean {
    return this.#leader === this.#tab;
  }

  join(reader: SharedReader<EventType>, since: string | undefined): void {
    const first = this.#readers.size === 0;
    this.#readers.add(reader);
    reader.onClose(() => this.#leave(reader));
    this.#cursor ??= since;

    if (first) this.#start();
    else this.#changed();
  }

  #leave(reader: SharedReader<EventType>): void {
    if (!this.#readers.delete(reader)) return;
    if (this.#readers.size > 0) this.#changed();
    else this.#stop();
  }

  #start(): void {
    const create = this.#platform.channel;
    if (create === undefined) {
      // Alone: this tab reads a stream of its own.
      this.#lead();
      return;
    }

    const channel = create(this.#name);
    channel.addEventListener("message", (event) => this.#heard(event.data));
    this.#channel = channel;
    this.#stopHide = this.#platform.onPageHide?.(() => this.#leaving());
    this.#ask();
  }

  #stop(): void {
    this.#clearTimer();
    this.#close();
    this.#remote.clear();
    this.#leaving();
    this.#leader = undefined;
    this.#claims = undefined;
    this.#stopHide?.();
    this.#stopHide = undefined;
    this.#channel?.close();
    this.#channel = undefined;
    this.#forget();
  }

  // leaving tells the other tabs that this one goes: the reading tab gives up
  // the role, and a listening tab withdraws what it wanted.
  #leaving(): void {
    // The stream's own place may be past the last event posted: a frame
    // that holds only the cursor can arrive in a chunk after it.
    if (this.#leading) this.#post({ kind: "resign", from: this.#tab, cursor: this.#stream?.lastEventId ?? this.#cursor });
    else this.#post({ kind: "demand", from: this.#tab, demands: [] });
  }

  // ── choosing the reading tab ─────────────────────────────────────────

  // ask asks who reads, and claims the role when nobody answers in time. A
  // tab that already listens says again what it wants, because the reading
  // tab may be a new one or may have dropped it, such as after this page came
  // back from the browser's back-forward cache.
  #ask(): void {
    this.#answered = false;
    this.#post({ kind: "probe", from: this.#tab });
    if (this.#leader !== undefined) this.#post({ kind: "demand", from: this.#tab, demands: this.#demands() });
    this.#later(this.#timing.answer, () => {
      if (this.#answered) this.#later(this.#timing.check, () => this.#ask());
      else this.#claim();
    });
  }

  #claim(): void {
    this.#leader = undefined;
    this.#claims = new Set([this.#tab]);
    this.#post({ kind: "claim", from: this.#tab });
    this.#later(this.#timing.claim, () => {
      const claims = this.#claims;
      this.#claims = undefined;
      if (claims === undefined) return;
      // The smallest id takes the role. Every tab that claimed computes the
      // same answer from the same claims, so exactly one of them leads.
      if ([...claims].sort()[0] === this.#tab) {
        this.#lead();
        return;
      }
      // Another tab takes the role. If it never says so, this one asks again.
      this.#ask();
    });
  }

  #lead(): void {
    this.#clearTimer();
    this.#claims = undefined;
    this.#leader = this.#tab;
    this.#remote.clear();
    this.#post({ kind: "lead", from: this.#tab });
    this.#changed();
  }

  // follow makes another tab the reading tab for this one.
  #follow(leader: string): void {
    const fresh = this.#leader !== leader;
    if (this.#leading) {
      this.#close();
      this.#remote.clear();
    }
    this.#leader = leader;
    this.#claims = undefined;
    this.#answered = true;
    if (fresh) {
      this.#post({ kind: "demand", from: this.#tab, demands: this.#demands() });
      this.#later(this.#timing.check, () => this.#ask());
    }
  }

  #later(ms: number, then: () => void): void {
    this.#clearTimer();
    this.#timer = setTimeout(() => {
      this.#timer = undefined;
      then();
    }, ms);
  }

  #clearTimer(): void {
    if (this.#timer !== undefined) clearTimeout(this.#timer);
    this.#timer = undefined;
  }

  #heard(data: unknown): void {
    if (!isMessage(data) || data.from === this.#tab || this.#readers.size === 0) return;

    switch (data.kind) {
      case "probe":
        if (this.#leading) this.#post({ kind: "lead", from: this.#tab });
        break;
      case "claim":
        if (this.#leading) this.#post({ kind: "lead", from: this.#tab });
        else this.#claims?.add(data.from);
        break;
      case "lead":
        // Two reading tabs: the one with the larger id stops. A tab that is
        // only claiming the role follows a tab that already reads, whatever
        // their ids, so a slow answer costs no handover.
        if (this.#leading && this.#tab < data.from) {
          this.#post({ kind: "lead", from: this.#tab });
          break;
        }
        this.#follow(data.from);
        break;
      case "resign":
        if (data.from !== this.#leader) break;
        this.#cursor = data.cursor ?? this.#cursor;
        this.#claim();
        break;
      case "demand":
        if (!this.#leading) break;
        if (data.demands.length === 0) this.#remote.delete(data.from);
        else this.#remote.set(data.from, data.demands);
        this.#changed();
        break;
      case "event":
        if (data.from !== this.#leader || this.#leading) break;
        this.#cursor = data.cursor ?? this.#cursor;
        this.#deliver(data.event, this.#cursor);
        break;
      case "ended":
        if (data.from !== this.#leader || this.#leading) break;
        this.#fail(data.failure);
        break;
    }
  }

  // ── the stream ───────────────────────────────────────────────────────

  // changed says that what this tab wants has moved. A listening tab tells
  // the reading tab, and the reading tab opens the stream again shortly.
  #changed(): void {
    if (!this.#leading) {
      if (this.#leader !== undefined) this.#post({ kind: "demand", from: this.#tab, demands: this.#demands() });
      return;
    }
    if (this.#settle !== undefined) clearTimeout(this.#settle);
    this.#settle = setTimeout(() => {
      this.#settle = undefined;
      this.#apply();
    }, this.#timing.settle);
  }

  #demands(): Demand[] {
    return [...this.#readers].map((reader) => reader.demand);
  }

  // apply opens the stream for the union of every demand, unless the stream
  // already open carries all of it. A stream that carries more is kept: a
  // subscription that ended costs no reconnection, and a widening one opens
  // the stream for the union as it is then.
  #apply(): void {
    if (!this.#leading || this.#readers.size === 0) return;
    const union = unionOf([...this.#demands(), ...[...this.#remote.values()].flat()]);
    if (this.#stream !== undefined && this.#opened !== undefined && covers(this.#opened, union)) return;

    this.#close();
    this.#opened = union;
    void this.#read(union);
  }

  #close(): void {
    if (this.#settle !== undefined) clearTimeout(this.#settle);
    this.#settle = undefined;
    const stream = this.#stream;
    this.#stream = undefined;
    this.#opened = undefined;
    // A frame that holds only the cursor may arrive after the last event, and
    // no event carries it here.
    this.#cursor = stream?.lastEventId ?? this.#cursor;
    stream?.close();
  }

  async #read(union: Demand): Promise<void> {
    // A placeholder, so a close that lands while the person is asked for
    // ends this read and not a later one.
    const asked = new AbortController();
    const placeholder: EventSubscription = {
      lastEventId: undefined,
      close: () => asked.abort(),
      [Symbol.asyncIterator]: async function* () {},
    };
    this.#stream = placeholder;

    // The tab that takes over may hold a cookie of another person by now.
    // The subscription asks before each reconnection and not before the
    // first connection, so the first is asked here.
    let now: string | undefined;
    try {
      now = await currentPrincipal(this.#transport, asked.signal);
    } catch (thrown) {
      if (this.#stream === placeholder) this.#end(failureOf(thrown));
      return;
    }
    if (this.#stream !== placeholder) return;
    if (now !== this.#principal) {
      this.#end({
        status: 401,
        body: undefined,
        message: "the stream ended: the credential that opened it now belongs to another subject",
      });
      return;
    }

    const stream = subscribe(this.#transport, {
      ...(union.types === undefined ? {} : { types: union.types as readonly EventType[] }),
      ...(union.interval === undefined ? {} : { interval: union.interval }),
      ...(this.#cursor === undefined ? {} : { since: this.#cursor }),
      principal: this.#principal,
    });
    this.#stream = stream;

    try {
      for await (const event of stream) {
        if (this.#stream !== stream) return;
        this.#cursor = stream.lastEventId ?? this.#cursor;
        this.#post({ kind: "event", from: this.#tab, event, cursor: this.#cursor });
        this.#deliver(event, this.#cursor);
      }
    } catch (thrown) {
      if (this.#stream === stream) this.#end(failureOf(thrown));
    }
  }

  // end passes the server's ending to every subscription in every tab. Each
  // consumer decides what to do, as with a stream of its own.
  #end(failure: Failure): void {
    this.#stream = undefined;
    this.#opened = undefined;
    this.#remote.clear();
    this.#post({ kind: "ended", from: this.#tab, failure });
    this.#fail(failure);
  }

  #deliver(event: StreamEvent, cursor: string | undefined): void {
    for (const reader of this.#readers) reader.push(event, cursor);
  }

  #fail(failure: Failure): void {
    for (const reader of [...this.#readers]) reader.fail(failure);
  }

  #post(message: Message): void {
    this.#channel?.postMessage(message);
  }
}

// tabId is a random id for one tab's part in one shared stream. Comparing two
// is how the tabs agree which one reads. `crypto.randomUUID` exists only in a
// secure context, and `getRandomValues` everywhere.
const tabId = (): string =>
  Array.from(crypto.getRandomValues(new Uint8Array(16)), (byte) => byte.toString(16).padStart(2, "0")).join("");

/**
 * What one consumer iterates. It looks like a stream of its own: the events
 * it asked for, in order, and the ending the server chose.
 */
class SharedReader<T extends EventType> implements EventSubscription<T> {
  lastEventId: string | undefined;
  readonly demand: Demand;

  readonly #types: ReadonlySet<string> | undefined;
  readonly #origins: ReadonlySet<string> | undefined;
  readonly #queue: { readonly event: StreamEvent<T>; readonly cursor: string | undefined }[] = [];
  readonly #controller = new AbortController();
  readonly #closers: (() => void)[] = [];
  #failure: Failure | undefined;
  #wake: (() => void) | undefined;
  #closed = false;

  constructor(options: EventsOptions<EventType>) {
    this.lastEventId = options.since;
    const types = options.types !== undefined && options.types.length > 0 ? [...options.types].sort() : undefined;
    this.demand = { types, interval: options.interval };
    this.#types = types === undefined ? undefined : new Set(types);
    this.#origins = options.origins !== undefined && options.origins.length > 0 ? new Set(options.origins) : undefined;
  }

  get closed(): boolean {
    return this.#closed;
  }

  get signal(): AbortSignal {
    return this.#controller.signal;
  }

  onClose(closer: () => void): void {
    if (this.#closed) closer();
    else this.#closers.push(closer);
  }

  push(event: StreamEvent, cursor: string | undefined): void {
    if (this.#closed || this.#failure !== undefined || !this.#wants(event)) return;
    // The shared stream carries the union of every demand, and this reader
    // keeps what it asked for, so the narrowing to T holds.
    this.#queue.push({ event: event as StreamEvent<T>, cursor });
    this.#notify();
  }

  fail(failure: Failure): void {
    if (this.#closed || this.#failure !== undefined) return;
    this.#failure = failure;
    this.#notify();
    this.#detach();
  }

  close(): void {
    if (this.#closed) return;
    this.#closed = true;
    this.#controller.abort();
    this.#queue.length = 0;
    this.#notify();
    this.#detach();
  }

  async *[Symbol.asyncIterator](): AsyncGenerator<StreamEvent<T>, void, undefined> {
    try {
      for (;;) {
        const next = this.#queue.shift();
        if (next !== undefined) {
          this.lastEventId = next.cursor ?? this.lastEventId;
          yield next.event;
          continue;
        }
        if (this.#closed) return;
        if (this.#failure !== undefined) throw errorOf(this.#failure);
        await new Promise<void>((resolve) => {
          this.#wake = resolve;
        });
      }
    } finally {
      this.close();
    }
  }

  #wants(event: StreamEvent): boolean {
    // The server sends this one past any narrowing, and so does this reader.
    if (event.type === CONTINUITY_LOST) return true;
    if (this.#types !== undefined && !this.#types.has(event.type)) return false;
    if (this.#origins === undefined) return true;

    // A withdrawn frame names its origin in the payload and has no envelope.
    const origin =
      event.type === ORIGIN_WITHDRAWN
        ? event.payload.origin
        : event.envelope === undefined
          ? undefined
          : originOf(event.envelope.subject, event.envelope.source);
    return origin !== undefined && this.#origins.has(origin);
  }

  #notify(): void {
    const wake = this.#wake;
    this.#wake = undefined;
    wake?.();
  }

  #detach(): void {
    const closers = this.#closers.splice(0);
    for (const closer of closers) closer();
  }
}

// unionOf is one demand that asks for everything each demand asks for: no
// type narrowing if one of them has none, and the shortest window.
const unionOf = (demands: readonly Demand[]): Demand => {
  let types: Set<string> | undefined = new Set();
  let interval: number | undefined;

  for (const demand of demands) {
    if (demand.types === undefined) types = undefined;
    else if (types !== undefined) for (const type of demand.types) types.add(type);

    const window = demand.interval ?? SERVER_DEFAULT_INTERVAL;
    interval = interval === undefined ? window : Math.min(interval, window);
  }

  return {
    types: types === undefined ? undefined : [...types].sort(),
    // The server's own window is asked for by naming none.
    interval: interval === SERVER_DEFAULT_INTERVAL ? undefined : interval,
  };
};

// covers says whether a stream opened for `opened` carries everything `wanted`
// asks for: every type, and a window no longer than the one asked for.
const covers = (opened: Demand, wanted: Demand): boolean => {
  const window = (demand: Demand): number => demand.interval ?? SERVER_DEFAULT_INTERVAL;
  if (window(opened) > window(wanted)) return false;
  if (opened.types === undefined) return true;
  if (wanted.types === undefined) return false;
  const types = new Set(opened.types);
  return wanted.types.every((type) => types.has(type));
};

// originOf reads the origin an event came from, as `kind:id`. The envelope's
// subject is that origin. A relayed envelope without one still has its
// source, `urn:prusa:site:<site>/<kind>/<id>`.
const originOf = (subject: string | undefined, source: string): string | undefined => {
  if (subject !== undefined && subject !== "") return subject;
  const parts = source.split("/");
  const id = parts.at(-1);
  const kind = parts.at(-2);
  return parts.length < 3 || id === undefined || kind === undefined ? undefined : `${kind}:${id}`;
};

// currentPrincipal asks the server who the cookie belongs to now. A 401 means
// nobody, and any other refusal is thrown.
const currentPrincipal = async (transport: Transport, signal: AbortSignal): Promise<string | undefined> => {
  try {
    const answer = await transport.call<{ principal?: unknown }>(operationTable["get-principal"], {}, signal);
    return typeof answer.principal === "string" ? answer.principal : undefined;
  } catch (thrown) {
    if (thrown instanceof ApiError && thrown.status === 401) return undefined;
    throw thrown;
  }
};

const failureOf = (thrown: unknown): Failure =>
  thrown instanceof ApiError
    ? { status: thrown.status, body: thrown.body, message: thrown.message }
    : { status: undefined, body: undefined, message: thrown instanceof Error ? thrown.message : String(thrown) };

// errorOf turns a failure back into what a stream of its own would throw: an
// ApiError when the server refused or ended it, and an Error otherwise.
const errorOf = (failure: Failure): Error =>
  failure.status === undefined ? new Error(failure.message) : new ApiError(failure.status, failure.body, failure.message);

// isMessage checks what another tab posted. It is the same origin, but it may
// be another release of this page, so the shape is read and not assumed.
const isMessage = (value: unknown): value is Message => {
  if (value === null || typeof value !== "object") return false;
  const message = value as {
    kind?: unknown;
    from?: unknown;
    demands?: unknown;
    event?: unknown;
    failure?: unknown;
    cursor?: unknown;
  };
  if (typeof message.from !== "string") return false;

  switch (message.kind) {
    case "probe":
    case "claim":
    case "lead":
      return true;
    case "resign":
      return message.cursor === undefined || typeof message.cursor === "string";
    case "demand":
      return Array.isArray(message.demands) && message.demands.every(isDemand);
    case "event":
      return isStreamEvent(message.event);
    case "ended":
      return isFailure(message.failure);
    default:
      return false;
  }
};

const isDemand = (value: unknown): value is Demand => {
  if (value === null || typeof value !== "object") return false;
  const demand = value as { types?: unknown; interval?: unknown };
  const types =
    demand.types === undefined || (Array.isArray(demand.types) && demand.types.every((type) => typeof type === "string"));
  return types && (demand.interval === undefined || typeof demand.interval === "number");
};

const isStreamEvent = (value: unknown): value is StreamEvent => {
  if (value === null || typeof value !== "object") return false;
  const event = value as { type?: unknown; envelope?: unknown };
  if (typeof event.type !== "string") return false;
  if (event.envelope === undefined) {
    return (
      event.type === CONTINUITY_LOST ||
      (event.type === ORIGIN_WITHDRAWN &&
        typeof (event as { payload?: { origin?: unknown } }).payload?.origin === "string")
    );
  }
  return (
    event.envelope !== null &&
    typeof event.envelope === "object" &&
    typeof (event.envelope as { source?: unknown }).source === "string"
  );
};

const isFailure = (value: unknown): value is Failure => {
  if (value === null || typeof value !== "object") return false;
  const failure = value as { status?: unknown; body?: unknown; message?: unknown };
  return (
    typeof failure.message === "string" &&
    (failure.status === undefined || typeof failure.status === "number") &&
    (failure.body === undefined || typeof failure.body === "string" || isProblem(failure.body))
  );
};
