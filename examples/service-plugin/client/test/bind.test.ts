import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

import { ApiError, PrusaConnect } from "@prusa/connect";

import { bind, operationTable } from "../dist/index.js";

// The module's client, bound onto a core client whose fetch is a stub, so the
// test reads the request the transport built and never opens a socket.

const MOUNT = "https://printers.local:8080/api/v1/modules/com.github.aux.acme.example";

interface Call {
  readonly url: string;
  readonly method: string;
  readonly headers: Headers;
  readonly body: string | undefined;
}

// stubbed answers every call with the one response given and records what it
// was asked.
const stubbed = (answer: () => Response): { calls: Call[]; local: PrusaConnect } => {
  const calls: Call[] = [];
  const fetch: typeof globalThis.fetch = async (input, init) => {
    calls.push({
      url: String(input),
      method: init?.method ?? "GET",
      headers: new Headers(init?.headers),
      body: typeof init?.body === "string" ? init.body : undefined,
    });
    return answer();
  };
  const local = new PrusaConnect({ url: "https://printers.local:8080/", auth: "token-1", fetch, retryDelay: 0 });
  return { calls, local };
};

const json = (status: number, body: unknown): Response =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

const camel = (id: string): string => id.replace(/-([a-z0-9])/g, (_, c: string) => c.toUpperCase());

test("bind attaches every operation of the fragment as a method", () => {
  const fragment = JSON.parse(readFileSync(new URL("../../openapi.json", import.meta.url), "utf8")) as {
    paths: Record<string, Record<string, { operationId?: string }>>;
  };

  const ids: string[] = [];
  for (const item of Object.values(fragment.paths)) {
    for (const op of Object.values(item)) {
      if (typeof op === "object" && typeof op.operationId === "string") ids.push(op.operationId);
    }
  }
  assert.ok(ids.length >= 10, `the fragment declares ${ids.length} operations, which is fewer than expected`);

  const example = bind(stubbed(() => json(200, {})).local);
  for (const id of ids) {
    assert.ok(id in operationTable, `${id} is not in the operation table`);
    const method = (example as unknown as Record<string, unknown>)[camel(id)];
    assert.equal(typeof method, "function", `${id} has no ${camel(id)} method`);
  }
  assert.equal(Object.keys(operationTable).length, ids.length, "the table holds an operation the fragment does not");
});

test("a call reaches the rebased path with the client's credential", async () => {
  const { calls, local } = stubbed(() => json(200, { spools: ["petg-black", "pla-galaxy"], principal: "alice" }));

  const page = await bind(local).listSpools();

  assert.deepEqual(page.spools, ["petg-black", "pla-galaxy"]);
  assert.equal(calls.length, 1);
  const [call] = calls;
  assert.ok(call);
  assert.equal(call.url, `${MOUNT}/spools`);
  assert.equal(call.method, "GET");
  assert.equal(call.headers.get("authorization"), "Bearer token-1");
});

test("a path parameter declared on the path item fills the template", async () => {
  const { calls, local } = stubbed(() => json(200, { value: "x", version: 3 }));

  const record = await bind(local).getState({ path: { key: "shelf/a" } });

  assert.equal(record.version, 3);
  const [call] = calls;
  assert.ok(call);
  assert.equal(call.url, `${MOUNT}/records/shelf%2Fa`);
});

test("a text body is sent as the fragment's media type", async () => {
  const { calls, local } = stubbed(() => json(201, { created: "pla-galaxy", principal: "alice" }));

  const created = await bind(local).createSpool({ body: "pla-galaxy" });

  assert.equal(created.created, "pla-galaxy");
  const [call] = calls;
  assert.ok(call);
  assert.equal(call.method, "POST");
  assert.equal(call.headers.get("content-type"), "text/plain");
  assert.equal(call.body, "pla-galaxy");
});

test("a refusal is the core's ApiError carrying the module's own code", async () => {
  const problem = {
    type: "about:blank",
    status: 404,
    code: "example.spool/NOT_FOUND",
    title: "no such spool",
    detail: "the shelf holds no spool by that name",
    params: {},
    retryable: false,
    trace_id: "trace-1",
  };
  const { local } = stubbed(
    () => new Response(JSON.stringify(problem), { status: 404, headers: { "content-type": "application/problem+json" } }),
  );

  await assert.rejects(bind(local).getState({ path: { key: "missing" } }), (err: unknown) => {
    assert.ok(err instanceof ApiError);
    assert.equal(err.status, 404);
    assert.equal(err.code, "example.spool/NOT_FOUND");
    assert.equal(err.traceId, "trace-1");
    return true;
  });
});
