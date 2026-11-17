// The slice of Node's built-in modules the tests call. Declared here rather
// than through @types/node, so the package's dev tree stays at the compiler
// and the types generator (ADR-20260909-c7x rule 3).

declare module "node:test" {
  export function test(name: string, fn: () => void | Promise<void>): void;
}

declare module "node:assert/strict" {
  function assert(value: unknown, message?: string): asserts value;
  namespace assert {
    function equal<T>(actual: unknown, expected: T, message?: string): asserts actual is T;
    function notEqual(actual: unknown, expected: unknown, message?: string): void;
    function deepEqual<T>(actual: unknown, expected: T, message?: string): asserts actual is T;
    function ok(value: unknown, message?: string): asserts value;
    function match(value: string, pattern: RegExp, message?: string): void;
    function rejects(promise: Promise<unknown>, expected?: unknown, message?: string): Promise<void>;
    function fail(message?: string): never;
  }
  export default assert;
}

declare module "node:fs" {
  export function readFileSync(path: string | URL, encoding: "utf8"): string;
}
