// The slice of Node's built-in modules the test calls, declared here rather
// than through @types/node so the dev tree stays at the compiler and the
// types generator, as the core package's does.

declare module "node:test" {
  export function test(name: string, fn: () => void | Promise<void>): void;
}

declare module "node:assert/strict" {
  function assert(value: unknown, message?: string): asserts value;
  namespace assert {
    function equal<T>(actual: unknown, expected: T, message?: string): asserts actual is T;
    function deepEqual<T>(actual: unknown, expected: T, message?: string): asserts actual is T;
    function ok(value: unknown, message?: string): asserts value;
    function rejects(promise: Promise<unknown>, expected?: unknown, message?: string): Promise<void>;
  }
  export default assert;
}

declare module "node:fs" {
  export function readFileSync(path: string | URL, encoding: "utf8"): string;
}
