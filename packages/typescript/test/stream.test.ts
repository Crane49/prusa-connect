import assert from "node:assert/strict";
import { test } from "node:test";

import { ApiError, PrusaConnect, type EventType, type StreamEvent } from "../dist/index.js";
import { empty, json, problem, type Stream, stream, type Stub, stubFetch } from "./stub.ts";

// The reader is exercised through the client with a stub fetch whose body is
// a stream the test writes into, so every test is the bytes a server would
// send and nothing about how the reader keeps its state.

const client = (stub: Stub): PrusaConnect =>
  new PrusaConnect({ url: "https://printers.local:8080", auth: "token-1", fetch: stub.fetch, retries: 0 });

const ARRIVED = "com.prusa.connect.device.arrived";
const SAMPLED = "com.prusa.connect.device.telemetry.sampled";
const LOST = "com.prusa.connect.stream.continuity.lost";
const WITHDRAWN = "com.prusa.connect.stream.origin.withdrawn";

const envelope = (type: string, data: unknown = { device_id: "p1" }): string =>
  JSON.stringify({
    specversion: "1.0",
    id: `01K3Q7XA26D0WM4T1ZJ8P5GH${type.length}`,
    source: "urn:prusa:site:workshop/device/p1",
    type,
    time: "2026-08-26T09:14:05Z",
    hops: 0,
    evclass: "state",
    data,
  });

// frame renders one event frame the way the server does: the id first when
// there is one, then the type, then the envelope.
const frame = (type: string, id?: string, data?: unknown): string =>
  `${id === undefined ? "" : `id: ${id}\n`}event: ${type}\ndata: ${envelope(type, data)}\n\n`;

// take reads n events and then closes the subscription, so the reader never
// asks the stub for a connection it has no answer for.
const take = async <T extends EventType>(
  events: AsyncIterable<StreamEvent<T>> & { close(): void },
  n: number,
): Promise<StreamEvent<T>[]> => {
  const seen: StreamEvent<T>[] = [];
  for await (const event of events) {
    seen.push(event);
    if (seen.length === n) break;
  }
  return seen;
};

const tick = (ms: number): Promise<void> => new Promise((resolve) => setTimeout(resolve, ms));

test("the token, the Accept header and the filters reach the request", async () => {
  const s = stream(frame(ARRIVED, "e1:device/p1:1"));
  const stub = stubFetch(s.response);

  const seen = await take(
    client(stub).events({ types: [ARRIVED, SAMPLED], origins: ["device:p1"], interval: 250, since: "e0:device/p1:0" }),
    1,
  );

  assert.equal(seen.length, 1);
  const [call] = stub.calls;
  assert(call);
  assert.equal(
    call.url,
    "https://printers.local:8080/api/v1/events?types=com.prusa.connect.device.arrived%2Ccom.prusa.connect.device.telemetry.sampled&origins=device%3Ap1&interval_ms=250",
  );
  assert.equal(call.method, "GET");
  assert.equal(call.headers.get("authorization"), "Bearer token-1");
  assert.equal(call.credentials, undefined);
  assert.equal(call.headers.get("accept"), "text/event-stream");
  assert.equal(call.headers.get("last-event-id"), "e0:device/p1:0");
});

test("session mode reads the stream with the cookie and no token, at reconnect too", async () => {
  const first = stream(frame(ARRIVED, "e1:device/p1:1"));
  const second = stream(frame(ARRIVED, "e1:device/p1:2"));
  const stub = stubFetch(first.response, second.response);
  const local = new PrusaConnect({ auth: { kind: "session" }, fetch: stub.fetch, retries: 0 });
  const events = local.events({ types: [ARRIVED], reconnectDelay: 1 });

  const iterator = events[Symbol.asyncIterator]();
  await iterator.next();
  first.end();
  const two = await iterator.next();
  assert.equal(two.value?.id, "e1:device/p1:2");

  assert.equal(stub.calls.length, 2);
  for (const call of stub.calls) {
    assert.equal(call.headers.has("authorization"), false);
    assert.equal(call.credentials, "same-origin");
    assert.equal(call.headers.get("accept"), "text/event-stream");
  }
  assert.equal(stub.calls[0]?.url, "/api/v1/events?types=com.prusa.connect.device.arrived");
  assert.equal(stub.calls[1]?.headers.get("last-event-id"), "e1:device/p1:1");
  await iterator.return?.();
});

test("no filter and no cursor sends a bare request", async () => {
  const s = stream(frame(ARRIVED));
  const stub = stubFetch(s.response);
  await take(client(stub).events(), 1);
  assert.equal(stub.calls[0]?.url, "https://printers.local:8080/api/v1/events");
  assert.equal(stub.calls[0]?.headers.has("last-event-id"), false);
});

test("a frame is parsed across chunk boundaries, with CRLF, comments and a retry line", async () => {
  const wire = `retry: 2000\r\n\r\n: heartbeat\r\n\r\n${frame(ARRIVED, "e1:device/p1:1").replaceAll("\n", "\r\n")}`;
  // Cut so one piece ends in the CR of a CRLF and another in the middle of the
  // JSON, which is what a socket does.
  const cut = wire.indexOf("\r\n", 20) + 1;
  const s = stream(wire.slice(0, cut), wire.slice(cut, cut + 40), wire.slice(cut + 40));
  const stub = stubFetch(s.response);

  const [event] = await take(client(stub).events(), 1);
  assert(event);
  assert.equal(event.type, ARRIVED);
  assert.equal(event.id, "e1:device/p1:1");
  assert.deepEqual(event.payload, { device_id: "p1" });
  assert.equal(event.envelope.source, "urn:prusa:site:workshop/device/p1");
});

test("multi-line data is joined with a newline and a bare CR ends a line", async () => {
  const body = envelope(ARRIVED, { a: 1 });
  const split = body.indexOf('"data"');
  const wire = `event: ${ARRIVED}\rdata: ${body.slice(0, split)}\rdata: ${body.slice(split)}\r\r`;
  const s = stream(wire);
  const stub = stubFetch(s.response);

  const [event] = await take(client(stub).events(), 1);
  assert(event);
  assert.deepEqual(event.payload, { a: 1 });
  assert.equal(event.id, undefined);
});

test("the last id is kept, a bare id frame moves it, telemetry does not, and it is the resume header", async () => {
  // The bare id frame comes in a chunk of its own. One that ends the chunk of
  // an event is that event's place, which the next test reads.
  const first = stream(frame(ARRIVED, "e1:device/p1:1") + frame(SAMPLED), "id: 01K3Q7W0:device/p1:2,device/p2:7\n\n");
  const second = stream();
  const stub = stubFetch(first.response, second.response);
  const events = client(stub).events({ reconnectDelay: 1 });

  const iterator = events[Symbol.asyncIterator]();
  const one = await iterator.next();
  assert.equal(one.done, false);
  assert.equal(events.lastEventId, "e1:device/p1:1");

  const two = await iterator.next();
  assert.equal(two.done, false);
  assert.equal(two.value?.type, SAMPLED);
  assert.equal(two.value?.id, undefined);
  assert.equal(events.lastEventId, "e1:device/p1:1");

  first.end();
  // The reader reconnects on its own; the third read waits on the new stream.
  const pending = iterator.next();
  await tick(30);
  assert.equal(stub.calls.length, 2);
  assert.equal(stub.calls[1]?.headers.get("last-event-id"), "01K3Q7W0:device/p1:2,device/p2:7");
  assert.equal(events.lastEventId, "01K3Q7W0:device/p1:2,device/p2:7");

  second.push(frame(ARRIVED, "e1:device/p1:3"));
  const three = await pending;
  assert.equal(three.value?.id, "e1:device/p1:3");
  events.close();
  await iterator.return?.();
});

test("the last event of a chunk carries the cursor-only frame that closes the window", async () => {
  const stub = stubFetch(
    stream(frame(ARRIVED, "e1:device/p1:1") + frame(ARRIVED, "e1:device/p2:7") + "id: e1:device/p1=1,device/p2=7\n\n").response,
  );
  const events = client(stub).events();
  const iterator = events[Symbol.asyncIterator]();

  await iterator.next();
  assert.equal(events.lastEventId, "e1:device/p1:1");
  await iterator.next();
  assert.equal(events.lastEventId, "e1:device/p1=1,device/p2=7");
  events.close();
  await iterator.return?.();
});

test("a dropped socket is reconnected and the consumer sees no gap", async () => {
  const first = stream(frame(ARRIVED, "e1:device/p1:1"));
  const second = stream(frame(ARRIVED, "e1:device/p1:2"));
  const stub = stubFetch(first.response, second.response);
  const events = client(stub).events({ reconnectDelay: 1 });

  const iterator = events[Symbol.asyncIterator]();
  await iterator.next();
  first.fail(new Error("connection reset"));
  const two = await iterator.next();
  assert.equal(two.value?.id, "e1:device/p1:2");
  assert.equal(stub.calls.length, 2);
  await iterator.return?.();
});

test("the wait before a reconnection is the server's retry when it sent one", async () => {
  const first = stream("retry: 40\n\n");
  const second = stream(frame(ARRIVED, "e1:device/p1:1"));
  // Five seconds of the reader's own, which the server's forty milliseconds replace.
  const stub = stubFetch(first.response, second.response);
  const events = client(stub).events({ reconnectDelay: 5000 });

  const iterator = events[Symbol.asyncIterator]();
  const pending = iterator.next();
  await tick(5);
  const ended = performance.now();
  first.end();
  const one = await pending;
  assert.equal(one.value?.type, ARRIVED);

  const [, reconnect] = stub.calls;
  assert(reconnect);
  const waited = reconnect.at - ended;
  assert.ok(waited >= 15, `the reader reconnected after ${waited}ms, under half the server's retry`);
  assert.ok(waited < 1000, `the reader reconnected after ${waited}ms, on its own delay rather than the server's`);
  await iterator.return?.();
});

test("the wait grows with each failed connection and stays under the ceiling", async () => {
  const refused = new Error("connection refused");
  const last = stream(frame(ARRIVED, "e1:device/p1:1"));
  const stub = stubFetch(refused, refused, refused, last.response);
  const events = client(stub).events({ reconnectDelay: 20, reconnectCeiling: 30 });

  const [event] = await take(events, 1);
  assert.equal(event?.type, ARRIVED);
  assert.equal(stub.calls.length, 4);

  const gaps = stub.calls.slice(1).map((call, i) => call.at - (stub.calls[i]?.at ?? 0));
  for (const gap of gaps) {
    assert.ok(gap >= 8, `a wait of ${gap}ms is under half the base`);
    assert.ok(gap < 500, `a wait of ${gap}ms is over the ceiling`);
  }
});

test("aborting the signal ends the iteration with no error and no reconnection", async () => {
  const s = stream(frame(ARRIVED, "e1:device/p1:1"));
  const stub = stubFetch(s.response);
  const controller = new AbortController();
  const events = client(stub).events({ signal: controller.signal, reconnectDelay: 1 });

  const seen: string[] = [];
  const done = (async () => {
    for await (const event of events) {
      seen.push(event.type);
      controller.abort();
    }
  })();
  await done;

  assert.deepEqual(seen, [ARRIVED]);
  await tick(20);
  assert.equal(stub.calls.length, 1);
  assert.equal(stub.calls[0]?.signal?.aborted, true);
});

test("close() ends the iteration while it waits on the socket", async () => {
  const s = stream(frame(ARRIVED, "e1:device/p1:1"));
  const stub = stubFetch(s.response);
  const events = client(stub).events({ reconnectDelay: 1 });

  const iterator = events[Symbol.asyncIterator]();
  await iterator.next();
  const pending = iterator.next();
  events.close();
  const end = await pending;
  assert.equal(end.done, true);
  await tick(20);
  assert.equal(stub.calls.length, 1);
});

test("leaving the loop closes the subscription", async () => {
  const s = stream(frame(ARRIVED, "e1:device/p1:1"), frame(ARRIVED, "e1:device/p1:2"));
  const stub = stubFetch(s.response);
  const events = client(stub).events({ reconnectDelay: 1 });

  const seen = await take(events, 1);
  assert.equal(seen.length, 1);
  s.end();
  await tick(20);
  assert.equal(stub.calls.length, 1);
});

test("an auth.expired frame ends the iteration with a 401 ApiError and no reconnection", async () => {
  const s = stream(frame(ARRIVED, "e1:device/p1:1"), "event: prusa-connect.auth.expired\ndata: {}\n\n");
  const stub = stubFetch(s.response);
  const events = client(stub).events({ reconnectDelay: 1 });

  const seen: string[] = [];
  await assert.rejects(
    (async () => {
      for await (const event of events) seen.push(event.type);
    })(),
    (err: unknown) => {
      assert(err instanceof ApiError);
      assert.equal(err.status, 401);
      assert.equal(err.code, undefined);
      assert.match(err.message, /no longer accepted/);
      return true;
    },
  );
  assert.deepEqual(seen, [ARRIVED]);
  await tick(20);
  assert.equal(stub.calls.length, 1);
});

test("a refusal at reconnect ends the iteration with the server's ApiError", async () => {
  const first = stream(frame(ARRIVED, "e1:device/p1:1"));
  const stub = stubFetch(first.response, problem(401, "core.auth/TOKEN_INVALID"));
  const events = client(stub).events({ reconnectDelay: 1 });

  const iterator = events[Symbol.asyncIterator]();
  await iterator.next();
  first.end();
  await assert.rejects(iterator.next(), (err: unknown) => {
    assert(err instanceof ApiError);
    assert.equal(err.status, 401);
    assert.equal(err.code, "core.auth/TOKEN_INVALID");
    return true;
  });
  assert.equal(stub.calls.length, 2);
});

// A reverse proxy answers 502 while the core behind it restarts, where a
// direct connection gets no answer at all. Both are a drop.
const badGateway = (): Response =>
  new Response("<html><body>502 Bad Gateway</body></html>", { status: 502, headers: { "content-type": "text/html" } });

test("a gateway error at reconnect is a drop, and the stream reconnects", async () => {
  const first = stream(frame(ARRIVED, "e1:device/p1:1"));
  const second = stream(frame(ARRIVED, "e1:device/p1:2"));
  const stub = stubFetch(first.response, badGateway(), empty(504), second.response);
  const events = client(stub).events({ reconnectDelay: 1 });

  const iterator = events[Symbol.asyncIterator]();
  await iterator.next();
  first.end();
  const next = await iterator.next();
  events.close();

  assert.equal(next.value?.id, "e1:device/p1:2");
  assert.equal(stub.calls.length, 4);
  assert.equal(stub.calls[3]?.headers.get("last-event-id"), "e1:device/p1:1");
});

test("a gateway error on the principal check is a drop, and the stream reconnects", async () => {
  const first = stream(frame(ARRIVED, "e1:device/p1:1"));
  const second = stream(frame(ARRIVED, "e1:device/p1:2"));
  const stub = stubFetch(first.response, badGateway(), json(200, { principal: "user:ada" }), second.response);
  const events = client(stub).events({ reconnectDelay: 1, principal: "user:ada" });

  const iterator = events[Symbol.asyncIterator]();
  await iterator.next();
  first.end();
  const next = await iterator.next();
  events.close();

  assert.equal(next.value?.id, "e1:device/p1:2");
  assert.equal(stub.calls[1]?.url, "https://printers.local:8080/api/v1/principal");
  assert.equal(stub.calls[2]?.url, "https://printers.local:8080/api/v1/principal");
  assert.equal(stub.calls.length, 4);
});

test("a refusal at connect is thrown before any event", async () => {
  const stub = stubFetch(problem(403, "core.auth/ACCESS_DENIED"));
  const events = client(stub).events({ origins: ["device:secret"] });
  await assert.rejects(events[Symbol.asyncIterator]().next(), (err: unknown) => {
    assert(err instanceof ApiError);
    assert.equal(err.status, 403);
    return true;
  });
});

test("a continuity-lost frame is yielded with its cursor as the id", async () => {
  const cursor = "01K3Q7W0:device/p1:9";
  const s = stream(`id: ${cursor}\nevent: ${LOST}\ndata: {"cursor":"${cursor}","dropped":12}\n\n`);
  const stub = stubFetch(s.response);
  const events = client(stub).events({ types: [ARRIVED] });

  const [event] = await take(events, 1);
  assert(event);
  assert.equal(event.type, LOST);
  assert.equal(event.id, cursor);
  assert.deepEqual(event.payload, { cursor, dropped: 12 });
  assert.equal(event.envelope, undefined);
  assert.equal(events.lastEventId, cursor);
});

test("a withdrawn frame is yielded with the origin it names", async () => {
  const s = stream(`id: e2:device/p1:3\nevent: ${WITHDRAWN}\ndata: {"origin":"device:p1"}\n\n`);
  const stub = stubFetch(s.response);
  const [event] = await take(client(stub).events({ types: [WITHDRAWN] }), 1);

  assert(event);
  assert.equal(event.type, WITHDRAWN);
  assert.equal(event.id, "e2:device/p1:3");
  assert.deepEqual(event.payload, { origin: "device:p1" });
  assert.equal(event.envelope, undefined);
});

test("a frame that does not carry an envelope ends the iteration with a TypeError", async () => {
  const s = stream(`event: ${ARRIVED}\ndata: not json\n\n`);
  const stub = stubFetch(s.response);
  await assert.rejects(take(client(stub).events(), 1), (err: unknown) => {
    assert(err instanceof TypeError);
    assert.match(err.message, /does not carry JSON/);
    return true;
  });
});

test("a stream opened for a subject reconnects when the credential still belongs to it", async () => {
  const first = stream(frame(ARRIVED, "e1:device/p1:1"));
  const second = stream(frame(ARRIVED, "e1:device/p1:2"));
  const stub = stubFetch(first.response, json(200, { principal: "user:ada" }), second.response);
  const events = client(stub).events({ reconnectDelay: 1, principal: "user:ada" });

  const iterator = events[Symbol.asyncIterator]();
  await iterator.next();
  first.end();
  const next = await iterator.next();
  events.close();

  assert.equal(next.done, false);
  assert.equal(stub.calls.length, 3);
  assert.equal(stub.calls[1]?.url, "https://printers.local:8080/api/v1/principal");
  assert.equal(stub.calls[2]?.headers.get("last-event-id"), "e1:device/p1:1");
});

test("a stream opened for a subject ends when the credential now belongs to another", async () => {
  const first = stream(frame(ARRIVED, "e1:device/p1:1"));
  const stub = stubFetch(first.response, json(200, { principal: "user:grace" }));
  const events = client(stub).events({ reconnectDelay: 1, principal: "user:ada" });

  const iterator = events[Symbol.asyncIterator]();
  await iterator.next();
  first.end();
  await assert.rejects(iterator.next(), (err: unknown) => {
    assert(err instanceof ApiError);
    assert.equal(err.status, 401);
    assert.equal(err.code, undefined);
    return true;
  });
  assert.equal(stub.calls.length, 2);
});

test("a stream opened for a subject ends when nobody is signed in any more", async () => {
  const first = stream(frame(ARRIVED, "e1:device/p1:1"));
  const stub = stubFetch(first.response, problem(401, "core.auth/UNAUTHENTICATED"));
  const events = client(stub).events({ reconnectDelay: 1, principal: "user:ada" });

  const iterator = events[Symbol.asyncIterator]();
  await iterator.next();
  first.end();
  await assert.rejects(iterator.next(), (err: unknown) => {
    assert(err instanceof ApiError);
    assert.equal(err.status, 401);
    assert.equal(err.code, undefined);
    return true;
  });
  assert.equal(stub.calls.length, 2);
});
