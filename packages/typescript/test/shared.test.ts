import assert from "node:assert/strict";
import { test } from "node:test";

import { ApiError, PrusaConnect, HttpTransport, type EventType, type StreamEvent } from "../dist/index.js";
import { type Channel, type Platform, StreamSharing } from "../dist/shared.js";
import { json } from "./stub.ts";

// Every tab of a browser reads one event stream (ADR-20261023-a52). Each test plays
// several tabs in one process: each tab is its own StreamSharing over its own
// fetch, and Node's BroadcastChannel carries the messages between them as a
// browser's does between tabs. Each test talks to a server of its own name, so
// the channels of two tests never meet.

const ARRIVED = "com.prusa.connect.device.arrived";
const SAMPLED = "com.prusa.connect.device.telemetry.sampled";
const WITHDRAWN = "com.prusa.connect.stream.origin.withdrawn";

// Short waits, so a handover takes milliseconds here and not seconds.
const FAST = { answer: 30, claim: 20, check: 150, settle: 5 };

const tick = (ms: number): Promise<void> => new Promise((resolve) => setTimeout(resolve, ms));

const until = async (what: string, done: () => boolean, ms = 3000): Promise<void> => {
  const end = Date.now() + ms;
  while (!done()) {
    if (Date.now() > end) assert.fail(`timed out waiting until ${what}`);
    await tick(5);
  }
};

const frame = (type: string, device: string, id?: string): string =>
  `${id === undefined ? "" : `id: ${id}\n`}event: ${type}\ndata: ${JSON.stringify({
    specversion: "1.0",
    id: `01K3Q7XA26D0WM4T1ZJ8P5GH${device}`,
    source: `urn:prusa:site:workshop/device/${device}`,
    subject: `device:${device}`,
    type,
    time: "2026-08-26T09:14:05Z",
    hops: 0,
    evclass: "state",
    data: { device_id: device },
  })}\n\n`;

const withdrawn = (origin: string, id: string): string =>
  `id: ${id}\nevent: ${WITHDRAWN}\ndata: ${JSON.stringify({ origin })}\n\n`;

/** One stream the server opened, which the test writes into. */
interface Opened {
  readonly tab: string;
  readonly url: string;
  readonly since: string | null;
  readonly signal: AbortSignal;
  push(chunk: string): void;
}

/** A server: who the cookie belongs to, and every stream it opened. */
class Server {
  readonly url: string;
  readonly opened: Opened[] = [];
  principal: string | undefined;

  constructor(name: string, principal = "user:ada") {
    this.url = `https://${name}.local`;
    this.principal = principal;
  }

  /** The streams nobody has closed. */
  get live(): Opened[] {
    return this.opened.filter((stream) => !stream.signal.aborted);
  }

  fetchFor(tab: string): typeof fetch {
    return async (input, init) => {
      const url = String(input);
      if (url.includes("/api/v1/principal")) {
        return this.principal === undefined
          ? json(401, { type: "about:blank", status: 401, code: "core.auth/TOKEN_INVALID", title: "no" })
          : json(200, { principal: this.principal });
      }
      if (!url.includes("/api/v1/events")) throw new Error(`the server has no answer for ${url}`);

      const encoder = new TextEncoder();
      let control: ReadableStreamDefaultController<Uint8Array> | undefined;
      const body = new ReadableStream<Uint8Array>({
        start: (controller) => {
          control = controller;
        },
      });
      const signal = init?.signal ?? new AbortController().signal;
      this.opened.push({
        tab,
        url,
        since: new Headers(init?.headers).get("last-event-id"),
        signal,
        push: (chunk) => {
          if (!signal.aborted) control?.enqueue(encoder.encode(chunk));
        },
      });
      return new Response(body, { status: 200, headers: { "content-type": "text/event-stream" } });
    };
  }
}

/** A channel the test can cut, the way a crashed or frozen tab stops hearing. */
class Cuttable implements Channel {
  cut = false;
  readonly #inner: BroadcastChannel;

  constructor(name: string) {
    this.#inner = new BroadcastChannel(name);
  }

  postMessage(message: Parameters<Channel["postMessage"]>[0]): void {
    if (!this.cut) this.#inner.postMessage(message);
  }

  addEventListener(type: "message", listener: (event: MessageEvent) => void): void {
    this.#inner.addEventListener(type, (event) => {
      if (!this.cut) listener(event);
    });
  }

  close(): void {
    this.#inner.close();
  }
}

/** One tab: its own sharing, its own fetch, and what each of its subscriptions heard. */
class Tab {
  readonly name: string;
  readonly channels: Cuttable[] = [];
  readonly #sharing: StreamSharing;
  readonly #transport: HttpTransport;

  constructor(server: Server, name: string, platform: Partial<Platform> = {}) {
    this.name = name;
    this.#transport = new HttpTransport({ url: server.url, auth: { kind: "session" }, fetch: server.fetchFor(name), retries: 0 });
    this.#sharing = new StreamSharing({
      channel: (channel) => {
        const made = new Cuttable(channel);
        this.channels.push(made);
        return made;
      },
      onPageHide: undefined,
      timing: FAST,
      ...platform,
    });
  }

  /** Subscribes and keeps reading in the background. */
  listen(options: Parameters<StreamSharing["events"]>[1] = {}): Listener {
    const subscription = this.#sharing.events(this.#transport, options, undefined);
    const listener: Listener = { events: [], error: undefined, done: false, close: () => subscription.close() };
    void (async () => {
      try {
        for await (const event of subscription) listener.events.push(event);
      } catch (thrown) {
        listener.error = thrown;
      }
      listener.done = true;
    })();
    return listener;
  }
}

interface Listener {
  readonly events: StreamEvent[];
  error: unknown;
  done: boolean;
  close(): void;
}

const closeAll = (listeners: readonly Listener[]): void => {
  for (const listener of listeners) listener.close();
};

test("ten tabs read one stream, and every tab hears each event", async () => {
  const server = new Server("ten-tabs");
  const tabs = Array.from({ length: 10 }, (_, at) => new Tab(server, `tab${at}`));
  const listeners = tabs.map((tab) => tab.listen({ types: [ARRIVED] }));

  await until("a stream is open", () => server.live.length === 1);
  // Long enough for every other tab to have asked, claimed and given up.
  await tick(200);
  assert.equal(server.opened.length, 1, "more than one tab opened a stream");

  server.live[0]?.push(frame(ARRIVED, "p1", "e1:device/p1:1"));
  await until("every tab heard the event", () => listeners.every((listener) => listener.events.length === 1));
  for (const listener of listeners) assert.equal(listener.events[0]?.type, ARRIVED);

  closeAll(listeners);
  await until("the stream is closed", () => server.live.length === 0);
});

test("the stream asks for the union, and each subscription keeps what it asked for", async () => {
  const server = new Server("union");
  const fleet = new Tab(server, "fleet").listen({ types: [ARRIVED], interval: 1000 });
  await until("the first stream is open", () => server.live.length === 1);
  assert.match(server.live[0]?.url ?? "", /types=com\.prusa\.connect\.device\.arrived&interval_ms=1000$/);

  server.live[0]?.push(frame(ARRIVED, "p1", "e1:device/p1:1"));
  await until("the fleet heard it", () => fleet.events.length === 1);

  // Another tab wants one printer's readings, at the server's own window.
  const printer = new Tab(server, "printer").listen({ types: [SAMPLED], origins: ["device:p2"] });
  await until("the stream is open again for both", () => server.opened.length === 2 && server.live.length === 1);
  const widened = server.live[0];
  assert(widened);
  assert.match(widened.url, /types=com\.prusa\.connect\.device\.arrived%2Ccom\.prusa\.connect\.device\.telemetry\.sampled$/);
  assert.equal(widened.since, "e1:device/p1:1", "the stream opened again did not resume");

  widened.push(frame(SAMPLED, "p1"));
  widened.push(frame(SAMPLED, "p2"));
  widened.push(frame(ARRIVED, "p2", "e1:device/p2:1"));
  await until("both heard their part", () => fleet.events.length === 2 && printer.events.length === 1);
  await tick(30);
  assert.deepEqual(
    fleet.events.map((event) => event.type),
    [ARRIVED, ARRIVED],
  );
  assert.deepEqual(
    printer.events.map((event) => event.envelope?.subject),
    ["device:p2"],
  );

  // The narrower demand left, and the wider stream is kept rather than opened again.
  printer.close();
  await tick(60);
  assert.equal(server.opened.length, 2);

  fleet.close();
});

test("a withdrawn frame goes only to a subscription that asked for its type and its origin", async () => {
  const server = new Server("withdrawn");
  const tab = new Tab(server, "tab");
  const all = tab.listen({});
  const other = tab.listen({ types: [ARRIVED] });
  const narrow = tab.listen({ origins: ["device:a"] });
  await until("the stream is open", () => server.live.length === 1);

  server.live[0]?.push(withdrawn("device:b", "e1:device/b:1"));
  server.live[0]?.push(withdrawn("device:a", "e1:device/a:1"));
  await until("the open ones heard it", () => all.events.length === 2 && narrow.events.length === 1);
  await tick(30);

  assert.deepEqual(
    all.events.map((event) => event.payload),
    [{ origin: "device:b" }, { origin: "device:a" }],
  );
  assert.deepEqual(
    narrow.events.map((event) => event.payload),
    [{ origin: "device:a" }],
  );
  assert.equal(other.events.length, 0);

  closeAll([all, other, narrow]);
});

test("the reading tab that closes hands the stream to another, from the last place", async () => {
  const server = new Server("handover");
  const tabs = [new Tab(server, "a"), new Tab(server, "b"), new Tab(server, "c")];
  const listeners = tabs.map((tab) => tab.listen({ types: [ARRIVED] }));
  await until("a stream is open", () => server.live.length === 1);
  await tick(100);

  const first = server.live[0];
  assert(first);
  first.push(frame(ARRIVED, "p1", "e1:device/p1:7"));
  await until("every tab heard it", () => listeners.every((listener) => listener.events.length === 1));

  const reading = tabs.findIndex((tab) => tab.name === first.tab);
  listeners[reading]?.close();

  await until("another tab opened the stream", () => server.live.length === 1 && server.opened.length === 2);
  const second = server.live[0];
  assert(second);
  assert.notEqual(second.tab, first.tab);
  assert.equal(second.since, "e1:device/p1:7");
  assert.equal(first.signal.aborted, true);

  second.push(frame(ARRIVED, "p1", "e1:device/p1:8"));
  const rest = listeners.filter((_, at) => at !== reading);
  await until("the other tabs heard the next event", () => rest.every((listener) => listener.events.length === 2));

  closeAll(rest);
});

test("a tab that takes over resumes every origin, from the cursor that closes the window", async () => {
  const server = new Server("handover-cursor");
  const tabs = [new Tab(server, "a"), new Tab(server, "b")];
  const listeners = tabs.map((tab) => tab.listen({ types: [ARRIVED] }));
  await until("a stream is open", () => server.live.length === 1);
  await tick(100);

  // A window of two origins ends with a frame that holds only the cursor of
  // both. Each event's own id names its origin alone.
  const first = server.live[0];
  assert(first);
  first.push(
    frame(ARRIVED, "p1", "e1:device/p1:7") + frame(ARRIVED, "p2", "e1:device/p2:3") + "id: e1:device/p1=7,device/p2=3\n\n",
  );
  await until("every tab heard both", () => listeners.every((listener) => listener.events.length === 2));

  const reading = tabs.findIndex((tab) => tab.name === first.tab);
  listeners[reading]?.close();

  await until("another tab opened the stream", () => server.live.length === 1 && server.opened.length === 2);
  const since = server.live[0]?.since;
  closeAll(listeners);
  assert.equal(since, "e1:device/p1=7,device/p2=3");
});

test("a tab that takes over resumes from a cursor that came in a chunk of its own", async () => {
  const server = new Server("handover-cursor-alone");
  const tabs = [new Tab(server, "a"), new Tab(server, "b")];
  const listeners = tabs.map((tab) => tab.listen({ types: [ARRIVED] }));
  await until("a stream is open", () => server.live.length === 1);
  await tick(100);

  const first = server.live[0];
  assert(first);
  first.push(frame(ARRIVED, "p1", "e1:device/p1:7") + frame(ARRIVED, "p2", "e1:device/p2:3"));
  await until("every tab heard both", () => listeners.every((listener) => listener.events.length === 2));
  first.push("id: e1:device/p1=7,device/p2=3\n\n");
  await tick(20);

  const reading = tabs.findIndex((tab) => tab.name === first.tab);
  listeners[reading]?.close();

  await until("another tab opened the stream", () => server.live.length === 1 && server.opened.length === 2);
  const since = server.live[0]?.since;
  closeAll(listeners);
  assert.equal(since, "e1:device/p1=7,device/p2=3");
});

test("a stream opened again for a wider union resumes from a cursor that came on its own", async () => {
  const server = new Server("reopen-cursor");
  const tab = new Tab(server, "a");
  const fleet = tab.listen({ types: [ARRIVED] });
  await until("a stream is open", () => server.live.length === 1);

  const first = server.live[0];
  assert(first);
  first.push(frame(ARRIVED, "p1", "e1:device/p1:7") + frame(ARRIVED, "p2", "e1:device/p2:3"));
  await until("both were heard", () => fleet.events.length === 2);
  // The cursor frame arrives in a chunk of its own.
  first.push("id: e1:device/p1=7,device/p2=3\n\n");
  await tick(20);

  const printer = tab.listen({ types: [SAMPLED] });
  await until("the stream is open again", () => server.opened.length === 2 && server.live.length === 1);
  const since = server.live[0]?.since;
  closeAll([fleet, printer]);
  assert.equal(since, "e1:device/p1=7,device/p2=3");
});

test("a reading tab that stops answering is replaced", async () => {
  const server = new Server("crash");
  const tabs = [new Tab(server, "a"), new Tab(server, "b")];
  const listeners = tabs.map((tab) => tab.listen({ types: [ARRIVED] }));
  await until("a stream is open", () => server.live.length === 1);
  await tick(100);

  const first = server.live[0];
  assert(first);
  // The reading tab goes silent without saying goodbye, the way a crashed or
  // frozen tab does.
  for (const channel of tabs.find((tab) => tab.name === first.tab)?.channels ?? []) channel.cut = true;

  await until("the other tab opened its own", () => server.opened.length === 2, 2000);
  const second = server.opened[1];
  assert(second);
  assert.notEqual(second.tab, first.tab);

  second.push(frame(ARRIVED, "p1", "e1:device/p1:2"));
  const survivor = listeners[tabs.findIndex((tab) => tab.name === second.tab)];
  await until("the survivor heard it", () => survivor?.events.length === 1);

  closeAll(listeners);
});

test("the tabs of two people never share a stream", async () => {
  const server = new Server("two-people");
  const ada = new Tab(server, "ada").listen({ types: [ARRIVED], principal: "user:ada" });
  await until("ada's stream is open", () => server.live.length === 1);

  // The cookie now belongs to bob, as after a sign-in in another tab.
  server.principal = "user:bob";
  const bob = new Tab(server, "bob").listen({ types: [ARRIVED] });
  await until("bob's stream is open", () => server.live.length === 2);
  await tick(100);

  const [adas, bobs] = server.opened;
  assert(adas && bobs);
  adas.push(frame(ARRIVED, "p1", "e1:device/p1:1"));
  bobs.push(frame(ARRIVED, "p2", "e1:device/p2:1"));
  await until("each heard one event", () => ada.events.length === 1 && bob.events.length === 1);
  await tick(50);
  assert.equal(ada.events.length, 1);
  assert.equal(bob.events.length, 1);
  assert.equal(ada.events[0]?.envelope?.subject, "device:p1");
  assert.equal(bob.events[0]?.envelope?.subject, "device:p2");

  ada.close();
  bob.close();
});

test("a tab that takes over for a person who is no longer signed in opens nothing", async () => {
  const server = new Server("signed-out");
  server.principal = "user:bob";
  const stale = new Tab(server, "stale").listen({ types: [ARRIVED], principal: "user:ada" });

  await until("the subscription ended", () => stale.done);
  assert.equal(server.opened.length, 0);
  assert(stale.error instanceof ApiError);
  assert.equal(stale.error.status, 401);
  assert.equal(stale.error.code, undefined);
});

test("the server ending the stream ends every subscription in every tab", async () => {
  const server = new Server("expired");
  const listeners = [new Tab(server, "a"), new Tab(server, "b"), new Tab(server, "c")].map((tab) =>
    tab.listen({ types: [ARRIVED] }),
  );
  await until("a stream is open", () => server.live.length === 1);
  await tick(100);

  server.live[0]?.push("event: prusa-connect.auth.expired\ndata: {}\n\n");
  await until("every subscription ended", () => listeners.every((listener) => listener.done));
  for (const listener of listeners) {
    assert(listener.error instanceof ApiError);
    assert.equal(listener.error.status, 401);
  }
});

test("without a BroadcastChannel each tab reads one stream for all its subscriptions", async () => {
  const server = new Server("alone");
  const alone = { channel: undefined };
  const one = new Tab(server, "one", alone);
  const two = new Tab(server, "two", alone);
  const listeners = [one.listen({ types: [ARRIVED] }), one.listen({ types: [SAMPLED] }), two.listen({ types: [ARRIVED] })];

  await until("two streams are open", () => server.live.length === 2);
  await tick(60);
  assert.equal(server.opened.length, 2);
  assert.deepEqual(server.opened.map((stream) => stream.tab).sort(), ["one", "two"]);

  closeAll(listeners);
  await until("both are closed", () => server.live.length === 0);
});

test("a client that shares reads one stream for its subscriptions, and its signal ends only its own", async () => {
  const server = new Server("client");
  const local = new PrusaConnect({ url: server.url, auth: { kind: "session" }, fetch: server.fetchFor("page"), retries: 0, shareEvents: true });
  const stopped = new AbortController();
  const module = local.withSignal(stopped.signal);

  const seen: EventType[] = [];
  const read = async (events: AsyncIterable<StreamEvent>): Promise<void> => {
    for await (const event of events) seen.push(event.type as EventType);
  };
  const app = local.events({ types: [ARRIVED] });
  void read(app);
  const ended = read(module.events({ types: [ARRIVED] }));

  await until("one stream is open", () => server.live.length === 1);
  await tick(100);
  assert.equal(server.opened.length, 1);

  stopped.abort();
  await ended;
  server.live[0]?.push(frame(ARRIVED, "p1", "e1:device/p1:1"));
  await until("the page heard it once", () => seen.length === 1);
  await tick(30);
  assert.equal(seen.length, 1);

  app.close();
  await until("the stream is closed", () => server.live.length === 0);
});
