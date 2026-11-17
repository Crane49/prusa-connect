import assert from "node:assert/strict";
import { test } from "node:test";

import { ApiError, PrusaConnect, HttpTransport, NetworkError, type Operation } from "../dist/index.js";
import { FakeXhr, install } from "./fake-xhr.ts";
import { stubFetch } from "./stub.ts";

const client = (): PrusaConnect =>
  new PrusaConnect({ url: "https://printers.local:8080", auth: "token-1", fetch: stubFetch().fetch, retryDelay: 0 });

const upload = (local: PrusaConnect, onProgress: (sent: number, total: number) => void, signal?: AbortSignal) =>
  local.uploadStorageObject(
    { path: { storage: "local" }, query: { path: "cube.bgcode" }, body: new Blob(["1234567"]), onProgress },
    signal,
  );

const flush = (): Promise<void> => new Promise((resolve) => setTimeout(resolve, 0));

test("an upload that asks for progress goes over XMLHttpRequest and reports each step", async () => {
  const restore = install();
  try {
    const seen: number[] = [];
    const answer = upload(client(), (sent) => seen.push(sent));
    await flush();

    const [xhr] = FakeXhr.sent;
    assert(xhr);
    assert.equal(xhr.method, "PUT");
    assert.equal(xhr.url, "https://printers.local:8080/api/v1/storages/local/content?path=cube.bgcode");
    assert.equal(xhr.headers.get("authorization"), "Bearer token-1");
    assert.equal(xhr.headers.get("content-type"), "application/octet-stream");

    xhr.progress(3, 7);
    xhr.progress(7, 7);
    xhr.answer(200, JSON.stringify({ bytes: 7, path: "cube.bgcode" }), { "content-type": "application/json" });

    assert.deepEqual(await answer, { bytes: 7, path: "cube.bgcode" });
    assert.deepEqual(seen, [3, 7]);
  } finally {
    restore();
  }
});

test("a refusal over XMLHttpRequest becomes an ApiError carrying the code", async () => {
  const restore = install();
  try {
    const answer = upload(client(), () => {});
    await flush();
    FakeXhr.sent[0]?.answer(
      413,
      JSON.stringify({ status: 413, title: "Too large", code: "core.http/BODY_TOO_LARGE" }),
      { "content-type": "application/problem+json" },
    );

    await assert.rejects(answer, (thrown: unknown) => thrown instanceof ApiError && thrown.status === 413);
  } finally {
    restore();
  }
});

test("an upload with progress whose connection failed is not sent again", async () => {
  const restore = install();
  try {
    const answer = upload(client(), () => {});
    await flush();
    FakeXhr.sent[0]?.fail();

    await assert.rejects(answer, (thrown: unknown) => thrown instanceof NetworkError);
    assert.equal(FakeXhr.sent.length, 1);
  } finally {
    restore();
  }
});

test("aborting the signal aborts the upload", async () => {
  const restore = install();
  try {
    const stop = new AbortController();
    const answer = upload(client(), () => {}, stop.signal);
    await flush();
    stop.abort();

    await assert.rejects(answer);
    assert.equal(FakeXhr.sent.length, 1);
  } finally {
    restore();
  }
});

test("a progress request carries no deadline even on an operation the document does not mark long-running", async () => {
  const restore = install();
  try {
    const transport = new HttpTransport({
      url: "https://printers.local:8080",
      auth: "token-1",
      retryDelay: 0,
      timeoutMs: 1,
    });
    const operation: Operation = {
      id: "put-thing",
      method: "PUT",
      path: "/api/v1/things/{id}",
      body: "application/octet-stream",
      response: "json",
      anonymous: false,
    };

    const answer = transport.call(operation, { path: { id: "1" }, body: new Blob(["1234567"]), onProgress: () => {} });
    await flush();
    await new Promise((resolve) => setTimeout(resolve, 20));
    FakeXhr.sent[0]?.answer(200, JSON.stringify({ ok: true }), { "content-type": "application/json" });

    assert.deepEqual(await answer, { ok: true });
  } finally {
    restore();
  }
});

test("an upload that asks for no progress still goes over fetch", async () => {
  const restore = install();
  try {
    const stub = stubFetch(new Response(JSON.stringify({ bytes: 7, path: "a" }), {
      status: 200,
      headers: { "content-type": "application/json" },
    }));
    const local = new PrusaConnect({ url: "https://printers.local:8080", auth: "t", fetch: stub.fetch, retryDelay: 0 });
    await local.uploadStorageObject({ path: { storage: "local" }, query: { path: "a" }, body: new Blob(["1234567"]) });

    assert.equal(stub.calls.length, 1);
    assert.equal(FakeXhr.sent.length, 0);
  } finally {
    restore();
  }
});
