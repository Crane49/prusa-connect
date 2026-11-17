import assert from "node:assert/strict";
import { test } from "node:test";

import { ApiError, type Auth, PrusaConnect, NetworkError } from "../dist/index.js";
import { empty, HANG, json, problem, type Stub, stubFetch } from "./stub.ts";

const client = (stub: Stub, auth: Auth = "token-1"): PrusaConnect =>
  new PrusaConnect({ url: "https://printers.local:8080/", auth, fetch: stub.fetch, retryDelay: 0 });

test("a static token is sent as a bearer and the trailing slash is dropped", async () => {
  const stub = stubFetch(json(200, { items: [], total: 0, offset: 0, limit: 100, issues: {} }));
  const page = await client(stub).listDevices({ query: { limit: 100, labels: ["site=lab", "row=2"] } });

  assert.equal(page.total, 0);
  assert.equal(stub.calls.length, 1);
  const [call] = stub.calls;
  assert(call);
  assert.equal(call.url, "https://printers.local:8080/api/v1/devices?limit=100&labels=site%3Dlab%2Crow%3D2");
  assert.equal(call.method, "GET");
  assert.equal(call.headers.get("authorization"), "Bearer token-1");
  assert.equal(call.credentials, undefined);
});

test("session mode sends no token, asks the browser for its cookie, and the paths are relative", async () => {
  const stub = stubFetch(
    json(200, { items: [], total: 0, offset: 0, limit: 100, issues: {} }),
    json(200, {}),
    json(200, { provider: "local", credentials: true, redirect: false, needs_setup: false }),
  );
  const local = new PrusaConnect({ auth: { kind: "session" }, fetch: stub.fetch, retryDelay: 0 });

  await local.listDevices({ query: { limit: 100 } });
  await local.sendDeviceCommand({ path: { device_id: "p1" }, body: { verb: "core.command.pause_print" } });
  await local.getAuthInfo();

  assert.equal(local.url, "");
  assert.equal(stub.calls.length, 3);
  for (const call of stub.calls) {
    assert.equal(call.headers.has("authorization"), false);
    assert.equal(call.credentials, "same-origin");
  }
  assert.equal(stub.calls[0]?.url, "/api/v1/devices?limit=100");
  assert.equal(stub.calls[1]?.url, "/api/v1/devices/p1/commands");
  assert.equal(stub.calls[1]?.method, "POST");
  assert.equal(stub.calls[2]?.url, "/api/v1/auth/info");
});

test("session mode keeps an origin it was given", async () => {
  const stub = stubFetch(json(200, { status: "ok" }));
  await client(stub, { kind: "session" }).getHealth();

  const [call] = stub.calls;
  assert(call);
  assert.equal(call.url, "https://printers.local:8080/api/v1/health");
  assert.equal(call.headers.has("authorization"), false);
  assert.equal(call.credentials, "same-origin");
});

test("a token provider is asked on every call", async () => {
  let n = 0;
  const stub = stubFetch(json(200, {}), json(200, {}));
  const local = client(stub, async () => `token-${(n += 1)}`);

  await local.getDevice({ path: { device_id: "p1" } });
  await local.getDevice({ path: { device_id: "p 2/x" } });

  assert.equal(stub.calls[0]?.headers.get("authorization"), "Bearer token-1");
  assert.equal(stub.calls[1]?.headers.get("authorization"), "Bearer token-2");
  assert.equal(stub.calls[1]?.url, "https://printers.local:8080/api/v1/devices/p%202%2Fx");
});

test("an operation the document publishes as anonymous sends no credential", async () => {
  const stub = stubFetch(json(200, { provider: "", credentials: false, redirect: false, needs_setup: false }));
  const info = await client(stub).getAuthInfo();

  assert.equal(info.provider, "");
  assert.equal(stub.calls[0]?.headers.has("authorization"), false);
});

test("a JSON body is sent as JSON and headers from the request reach the wire", async () => {
  const stub = stubFetch(json(200, { name: "warm", version: "1", steps: ["M104 S215"] }));
  await client(stub).putGcodePreset({
    path: { name: "warm" },
    header: { "If-Match": "\"1\"" },
    query: { dry_run: true },
    body: { steps: ["M104 S215"] },
  });

  const [call] = stub.calls;
  assert(call);
  assert.equal(call.method, "PUT");
  assert.equal(call.url, "https://printers.local:8080/api/v1/gcode-presets/warm?dry_run=true");
  assert.equal(call.headers.get("content-type"), "application/json");
  assert.equal(call.headers.get("if-match"), "\"1\"");
  assert.deepEqual(JSON.parse(call.body ?? ""), { steps: ["M104 S215"] });
});

test("a 204 answers undefined", async () => {
  const stub = stubFetch(empty(204));
  const answer = await client(stub).logout();
  assert.equal(answer, undefined);
});

test("a problem document becomes an ApiError carrying the code", async () => {
  const stub = stubFetch(problem(403, "core.auth/ACCESS_DENIED", { params: { object: "device:p1" } }));

  await assert.rejects(client(stub).getDevice({ path: { device_id: "p1" } }), (err: unknown) => {
    assert(err instanceof ApiError);
    assert.equal(err.status, 403);
    assert.equal(err.code, "core.auth/ACCESS_DENIED");
    assert.equal(err.retryable, false);
    assert.equal(err.traceId, "trace-1");
    assert.deepEqual(err.params, { object: "device:p1" });
    assert.equal(err.message, "the server refused with core.auth/ACCESS_DENIED");
    assert.equal(err.problem?.title, "refused");
    return true;
  });
});

test("a refusal that is not a problem document keeps its text and no code", async () => {
  const stub = stubFetch(new Response("<html>bad gateway</html>", { status: 502, headers: { "content-type": "text/html" } }));

  await assert.rejects(client(stub).getHealth(), (err: unknown) => {
    assert(err instanceof ApiError);
    assert.equal(err.status, 502);
    assert.equal(err.code, undefined);
    assert.equal(err.body, "<html>bad gateway</html>");
    assert.equal(err.message, "HTTP 502");
    return true;
  });
});

test("a GET whose connection failed is sent again, and a status is never retried", async () => {
  const stub = stubFetch(new TypeError("fetch failed"), json(200, { status: "ok" }));
  const health = await client(stub).getHealth();

  assert.equal(health.status, "ok");
  assert.equal(stub.calls.length, 2);

  const refused = stubFetch(problem(500, "core.printer/INTERNAL"), json(200, {}));
  await assert.rejects(client(refused).getHealth(), ApiError);
  assert.equal(refused.calls.length, 1);
});

test("a POST is never sent twice", async () => {
  const stub = stubFetch(new TypeError("fetch failed"), json(200, {}));

  await assert.rejects(
    client(stub).sendDeviceCommand({ path: { device_id: "p1" }, body: { verb: "core.command.pause_print" } }),
    (err: unknown) => {
      assert(err instanceof NetworkError);
      assert.equal(err.attempts, 1);
      assert(err.cause instanceof TypeError);
      return true;
    },
  );
  assert.equal(stub.calls.length, 1);
});

test("the retries run out and the last cause is reported", async () => {
  const stub = stubFetch(new TypeError("one"), new TypeError("two"), new TypeError("three"), json(200, {}));

  await assert.rejects(client(stub).getHealth(), (err: unknown) => {
    assert(err instanceof NetworkError);
    assert.equal(err.attempts, 3);
    assert.match(err.message, /3 attempts: three/);
    return true;
  });
  assert.equal(stub.calls.length, 3);
});

test("an aborted signal reaches fetch and stops the retry", async () => {
  const controller = new AbortController();
  const stub = stubFetch(new DOMException("aborted", "AbortError"));
  const local = client(stub);

  controller.abort();
  await assert.rejects(local.getHealth({}, controller.signal), NetworkError);
  assert.equal(stub.calls.length, 1);
  // The signal fetch is given joins the caller's with the attempt's deadline,
  // so what reaches it is already aborted and carries the caller's reason.
  assert.equal(stub.calls[0]?.signal?.aborted, true);
  assert.equal(stub.calls[0]?.signal?.reason, controller.signal.reason);
});

// The old backlog. Without a deadline a request that gets no answer at all waits
// for as long as the server takes, and a page waiting on it waits with it.
test("a request that is never answered is given up on, and every attempt gets the whole deadline", async () => {
  const stub = stubFetch(HANG, HANG, HANG);
  const local = new PrusaConnect({
    url: "https://printers.local:8080",
    auth: "token-1",
    fetch: stub.fetch,
    retryDelay: 0,
    timeoutMs: 20,
  });

  const started = performance.now();
  await assert.rejects(local.getHealth(), (err: unknown) => {
    assert(err instanceof NetworkError);
    assert.equal(err.attempts, 3);
    assert(err.cause instanceof DOMException);
    assert.equal(err.cause.name, "TimeoutError");
    return true;
  });

  // Three attempts of their own deadline each, and not one shared between them.
  assert.equal(stub.calls.length, 3);
  assert.ok(performance.now() - started >= 40, "the attempts shared one deadline");
});

test("the caller's signal wins over the deadline, and the request is not sent again", async () => {
  const controller = new AbortController();
  const stub = stubFetch(HANG, HANG, HANG);
  const local = new PrusaConnect({
    url: "https://printers.local:8080",
    auth: "token-1",
    fetch: stub.fetch,
    retryDelay: 0,
    timeoutMs: 10_000,
  });

  const reason = new Error("the person left the page");
  const answer = local.getHealth({}, controller.signal);
  controller.abort(reason);

  await assert.rejects(answer, (err: unknown) => {
    assert(err instanceof NetworkError);
    assert.equal(err.attempts, 1);
    assert.equal(err.cause, reason);
    return true;
  });
  assert.equal(stub.calls.length, 1);
});

// The event stream and the camera routes hand the Response over and the caller
// reads the body from it, so a deadline on the fetch would cut that read.
test("an operation whose answer is the response itself carries no deadline", async () => {
  const controller = new AbortController();
  const stub = stubFetch(HANG);
  const local = new PrusaConnect({
    url: "https://printers.local:8080",
    auth: "token-1",
    fetch: stub.fetch,
    retryDelay: 0,
    timeoutMs: 1,
  });

  const answer = local.streamCamera({ path: { camera: "cam-1" } }, controller.signal);
  await wait(30);
  assert.equal(stub.calls[0]?.signal, controller.signal);

  controller.abort();
  await assert.rejects(answer, NetworkError);
});

// Sending a file to a printer answers when the printer says the file arrived,
// so a deadline here abandons a transfer that is still running and the server
// then records a file the machine holds as one that never came.
test("an operation whose answer waits on a machine carries no deadline", async () => {
  const controller = new AbortController();
  const stub = stubFetch(HANG);
  const local = new PrusaConnect({
    url: "https://printers.local:8080",
    auth: "token-1",
    fetch: stub.fetch,
    retryDelay: 0,
    timeoutMs: 1,
  });

  const answer = local.sendDeviceFile({
    path: { device_id: "printer-1" },
    body: { storage: "local", source: "ring.bgcode", path: "usb/ring.bgcode" },
  }, controller.signal);
  await wait(30);
  assert.equal(stub.calls[0]?.signal, controller.signal);

  controller.abort();
  await assert.rejects(answer, NetworkError);
});

const wait = (ms: number): Promise<void> => new Promise((resolve) => setTimeout(resolve, ms));

test("a text answer is read as a string and a stream is handed over", async () => {
  const text = stubFetch(new Response("# HELP up\nup 1\n", { status: 200, headers: { "content-type": "text/plain" } }));
  assert.equal(await client(text).getMetrics(), "# HELP up\nup 1\n");

  const raw = stubFetch(new Response("frame", { status: 200, headers: { "content-type": "image/jpeg" } }));
  const response = await client(raw).getCameraSnapshot({ path: { camera: "cam-1" } });
  assert(response instanceof Response);
  assert.equal(await response.text(), "frame");
});

test("a missing path parameter is refused before anything is sent", async () => {
  const stub = stubFetch(json(200, {}));
  await assert.rejects(
    client(stub).getDevice({ path: {} as { device_id: string } }),
    (err: unknown) => err instanceof TypeError && /device_id is missing/.test(err.message),
  );
  assert.equal(stub.calls.length, 0);
});

test("a client bound to a signal aborts its requests in flight and sends none after", async () => {
  const stub = stubFetch(HANG, json(200, { items: [], total: 0, offset: 0, limit: 100, issues: {} }));
  const base = client(stub);
  const ended = new AbortController();
  const bound = base.withSignal(ended.signal);

  const inFlight = bound.listDevices();
  await new Promise((resolve) => setTimeout(resolve, 0));
  ended.abort();

  await assert.rejects(inFlight, (err: unknown) => {
    assert(err instanceof Error);
    assert.equal(err.name, "AbortError");
    assert.equal(err instanceof NetworkError, false);
    return true;
  });
  assert.equal(stub.calls.length, 1, "an aborted request was sent again");

  await assert.rejects(bound.listDevices(), (err: unknown) => {
    assert(err instanceof Error);
    assert.equal(err.name, "AbortError");
    return true;
  });
  assert.equal(stub.calls.length, 1, "a request was sent after the signal ended the client");

  // The client it was bound from is not ended with it.
  await base.listDevices();
  assert.equal(stub.calls.length, 2);
});

test("a 401 tells onUnauthorized before the call throws, and no other status does", async () => {
  const stub = stubFetch(json(200, { items: [], total: 0, offset: 0, limit: 100, issues: {} }), problem(401, "core.auth/UNAUTHENTICATED"), problem(403, "core.auth/FORBIDDEN"));
  let told = 0;
  const local = new PrusaConnect({
    auth: { kind: "session" },
    fetch: stub.fetch,
    retryDelay: 0,
    onUnauthorized: () => {
      told += 1;
    },
  });

  await local.listDevices();
  await assert.rejects(local.listDevices(), ApiError);
  assert.equal(told, 1);
  await assert.rejects(local.listDevices(), ApiError);
  assert.equal(told, 1);
});

test("a refusal is reported with its operation, before the call throws", async () => {
  const seen: Array<[string, string, number | undefined]> = [];
  const stub = stubFetch(problem(500, "core.x/Y", { trace_id: "abc" }));
  const sdk = new PrusaConnect({
    url: "https://printers.local:8080/",
    auth: "token-1",
    fetch: stub.fetch,
    retryDelay: 0,
    onFailure: (failure, operation) => seen.push([operation.id, operation.method, (failure as ApiError).status]),
  });

  await assert.rejects(sdk.getDevice({ path: { device_id: "d1" } }));

  assert.deepEqual(seen, [["get-device", "GET", 500]]);
});

test("a call its own caller aborted is not reported", async () => {
  let reported = 0;
  const stub = stubFetch(HANG);
  const controller = new AbortController();
  const sdk = new PrusaConnect({
    url: "https://printers.local:8080/",
    auth: "token-1",
    fetch: stub.fetch,
    retryDelay: 0,
    onFailure: () => {
      reported += 1;
    },
  });

  const pending = sdk.listDevices({ query: { limit: 1 } }, controller.signal);
  controller.abort();
  await assert.rejects(pending);

  assert.equal(reported, 0);
});
