// The shape the generated operation table and the hand-written transport
// agree on. The generator emits one Operation per route; the transport reads
// it to build the request and to decide how to read the answer.

/** An HTTP method, as the document spells it. */
export type Method = "GET" | "PUT" | "POST" | "DELETE" | "PATCH" | "HEAD" | "OPTIONS" | "TRACE";

/**
 * How a successful answer is read: parsed JSON, the text, the Response itself
 * for a stream or an image, or nothing for a 204.
 */
export type ResponseKind = "json" | "text" | "raw" | "none";

/** One route as the document declares it: what to send, and how to read the answer. */
export interface Operation {
  readonly id: string;
  readonly method: Method;
  // The route with its path parameters in braces, as the document writes it.
  readonly path: string;
  // The media type of the request body, absent when the operation takes none.
  readonly body?: string;
  readonly response: ResponseKind;
  // True for an operation the document publishes with an empty security
  // requirement: no credential is sent.
  readonly anonymous: boolean;
  // True for an operation whose answer waits on a machine: sending a file to a
  // printer returns when the printer says the file arrived. A deadline here
  // would abandon a transfer that is still running, and the server would then
  // record it as failed although the file reached the machine.
  readonly longRunning?: boolean;
}

/**
 * What a caller passes. The generated interface narrows each member to the
 * operation's own types; this is the width the transport works at.
 */
export interface OperationRequest {
  readonly path?: Readonly<Record<string, string | number | boolean>> | undefined;
  readonly query?: Readonly<Record<string, unknown>> | undefined;
  readonly header?: Readonly<Record<string, string | undefined>> | undefined;
  readonly body?: unknown;
  // Called as a request body goes up, with the bytes sent and the body's
  // length. Set, the request goes over XMLHttpRequest, see progress.ts.
  readonly onProgress?: ((sent: number, total: number) => void) | undefined;
}

/** What sends an operation and reads its answer. HttpTransport is the one the package ships. */
export interface Transport {
  call<T>(operation: Operation, request: OperationRequest, signal?: AbortSignal): Promise<T>;
}
