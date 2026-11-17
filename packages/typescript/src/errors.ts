import type { components } from "./generated/core-api.js";
import type { ErrorCode } from "./generated/errors.js";

/** The RFC 9457 problem document every refusal carries. */
export type Problem = components["schemas"]["ProblemError"];

/**
 * ApiError is what a refused request throws. Branch on `code`, which comes
 * from a published set, and never on the wording of `message`.
 */
export class ApiError extends Error {
  override readonly name = "ApiError";
  readonly status: number;
  // The registry code. A module's own code is a string the union does not
  // name, and a body that is not a problem document carries none.
  readonly code: ErrorCode | (string & {}) | undefined;
  readonly retryable: boolean;
  readonly params: Readonly<Record<string, unknown>>;
  // Quote it to an operator, who finds the same request in the log.
  readonly traceId: string | undefined;
  // The problem document, or whatever body the server sent instead.
  readonly body: Problem | string | undefined;

  constructor(status: number, body: Problem | string | undefined, message?: string) {
    const problem = typeof body === "object" ? body : undefined;
    super(message ?? messageOf(status, body));
    this.status = status;
    this.body = body;
    this.code = problem?.code;
    this.retryable = problem?.retryable ?? false;
    this.params = problem?.params ?? {};
    this.traceId = problem?.trace_id;
  }

  get problem(): Problem | undefined {
    return typeof this.body === "object" ? this.body : undefined;
  }
}

/**
 * NetworkError is what a request throws when no answer arrived: the
 * connection failed, was refused, or dropped before a status line.
 */
export class NetworkError extends Error {
  override readonly name = "NetworkError";
  readonly attempts: number;

  constructor(cause: unknown, attempts: number) {
    super(`request failed after ${attempts} attempt${attempts === 1 ? "" : "s"}: ${describe(cause)}`, {
      cause,
    });
    this.attempts = attempts;
  }
}

const messageOf = (status: number, body: Problem | string | undefined): string => {
  if (typeof body === "object") {
    return body.detail !== undefined && body.detail !== "" ? body.detail : `${body.code}: ${body.title}`;
  }
  return `HTTP ${status}`;
};

const describe = (cause: unknown): string => (cause instanceof Error ? cause.message : String(cause));

/**
 * isProblem reports whether a decoded body is a problem document: the fields
 * the schema requires are present with the types it gives them.
 */
export const isProblem = (body: unknown): body is Problem =>
  body !== null &&
  typeof body === "object" &&
  typeof (body as { code?: unknown }).code === "string" &&
  typeof (body as { status?: unknown }).status === "number" &&
  typeof (body as { title?: unknown }).title === "string";
