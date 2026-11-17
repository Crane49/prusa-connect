import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

import { PrusaConnect, errorCodes, eventClasses, eventTypes, operationTable } from "../dist/index.js";

// The committed inputs the generator reads, so this test asks the same
// question `generate-client-sdk -check` asks, from the consumer's side.
const read = (path: string): unknown => JSON.parse(readFileSync(new URL(path, import.meta.url), "utf8"));

const methods = ["get", "put", "post", "delete", "patch", "head", "options", "trace"];

const camel = (id: string): string => id.replace(/-([a-z0-9])/g, (_, c: string) => c.toUpperCase());

test("every operation in the document is a method on the client", () => {
  const document = read("../../../internal/transport/http/httpapi/openapi.json") as {
    paths: Record<string, Record<string, { operationId: string }>>;
  };
  const local = new PrusaConnect({ url: "https://printers.local", auth: "t" });

  const ids: string[] = [];
  for (const item of Object.values(document.paths)) {
    for (const [method, op] of Object.entries(item)) {
      if (methods.includes(method)) ids.push(op.operationId);
    }
  }

  assert.ok(ids.length >= 80, `the document declares ${ids.length} operations, which is fewer than expected`);

  for (const id of ids) {
    assert.ok(id in operationTable, `${id} is not in the operation table`);
    const name = camel(id);
    assert.equal(typeof (local as unknown as Record<string, unknown>)[name], "function", `${id} has no ${name} method`);
  }

  assert.equal(Object.keys(operationTable).length, ids.length, "the table holds an operation the document does not");
});

test("every registered error code is in the generated registry", () => {
  const registry = read("../../../docs/errors/error-codes.json") as { codes: { code: string; status: number }[] };

  assert.ok(registry.codes.length >= 50);
  for (const { code, status } of registry.codes) {
    const entry = (errorCodes as Record<string, { status: number } | undefined>)[code];
    assert.ok(entry, `${code} is not in errorCodes`);
    assert.equal(entry.status, status, `${code} carries the wrong status`);
  }
  assert.equal(Object.keys(errorCodes).length, registry.codes.length);
});

test("every published event type is in the generated union", () => {
  const vocabulary = read("../../../docs/vocabularies/event-types.json") as { entries: { type: string; class: string }[] };

  assert.ok(vocabulary.entries.length >= 10);
  for (const { type, class: cls } of vocabulary.entries) {
    assert.ok((eventTypes as readonly string[]).includes(type), `${type} is not in eventTypes`);
    assert.equal((eventClasses as Record<string, string>)[type], cls);
  }
  assert.equal(eventTypes.length, vocabulary.entries.length);
});

test("the package declares no runtime dependency, no lifecycle script, and the core's version", () => {
  const manifest = read("../package.json") as {
    version: string;
    type: string;
    dependencies?: unknown;
    scripts: Record<string, string>;
    exports: Record<string, unknown>;
  };

  assert.equal(manifest.type, "module");
  assert.equal(manifest.dependencies, undefined);
  assert.equal(manifest.version, readFileSync(new URL("../../../VERSION", import.meta.url), "utf8").trim());
  assert.ok("." in manifest.exports);
  for (const name of ["preinstall", "install", "postinstall", "prepare", "prepublish"]) {
    assert.equal(manifest.scripts[name], undefined, `package.json runs a ${name} script`);
  }
});
