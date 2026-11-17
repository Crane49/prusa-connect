import assert from "node:assert/strict";
import { test } from "node:test";

import { ApiError, errorCodes, isProblem, type Problem } from "../dist/index.js";

const refused: Problem = {
  type: "about:blank",
  status: 404,
  code: "core.printer/NOT_FOUND",
  title: "No such printer",
  detail: "printer p9 is not in the fleet",
  params: { device_id: "p9" },
  retryable: false,
  trace_id: "t-1",
};

test("an ApiError carries the problem document's fields", () => {
  const err = new ApiError(404, refused);

  assert.equal(err.name, "ApiError");
  assert(err instanceof Error);
  assert.equal(err.status, 404);
  assert.equal(err.code, "core.printer/NOT_FOUND");
  assert.equal(err.message, "printer p9 is not in the fleet");
  assert.deepEqual(err.params, { device_id: "p9" });
  assert.equal(err.retryable, false);
  assert.equal(err.traceId, "t-1");
  assert.equal(err.problem, refused);
});

test("a problem with no detail is named by its code and title", () => {
  const { detail: _detail, ...bare } = refused;
  const err = new ApiError(404, bare);
  assert.equal(err.message, "core.printer/NOT_FOUND: No such printer");
});

test("a body that is not a problem document leaves the code undefined", () => {
  const err = new ApiError(502, "upstream gone");

  assert.equal(err.code, undefined);
  assert.equal(err.body, "upstream gone");
  assert.equal(err.problem, undefined);
  assert.deepEqual(err.params, {});
  assert.equal(err.message, "HTTP 502");
});

test("the code narrows to the generated union and the registry answers it", () => {
  const err = new ApiError(404, refused);

  if (err.code !== undefined && err.code in errorCodes) {
    const entry = errorCodes[err.code as keyof typeof errorCodes];
    assert.equal(entry.status, 404);
    assert.equal(entry.retryable, false);
    assert.ok(entry.params.includes("device_id"), "the registry names device_id for NOT_FOUND");
  } else {
    assert.fail("core.printer/NOT_FOUND is not in the generated registry");
  }
});

test("isProblem asks for the fields the schema requires", () => {
  assert.equal(isProblem(refused), true);
  assert.equal(isProblem({ code: "x/Y" }), false);
  assert.equal(isProblem("text"), false);
  assert.equal(isProblem(null), false);
});
