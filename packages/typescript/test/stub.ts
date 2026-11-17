// A fetch that answers from a script and records what it was asked, so a test
// reads the request the transport built and never opens a socket.

export interface Call {
  // When the call arrived, from performance.now(), so a test can measure a wait.
  readonly at: number;
  readonly url: string;
  readonly method: string;
  readonly headers: Headers;
  readonly body: string | undefined;
  readonly signal: AbortSignal | null | undefined;
  // What the request asked the browser to do about cookies, when it asked.
  readonly credentials: RequestCredentials | undefined;
}

// The answer that never comes. A fetch given it waits for the signal and
// rejects with the signal's reason, which is what a real one does when a
// deadline or a caller aborts it.
export const HANG = Symbol("hang");

export type Answer = Response | Error | typeof HANG;

export interface Stub {
  readonly calls: Call[];
  readonly fetch: typeof fetch;
}

// stubFetch answers the nth call with answers[n]. An Error in the script
// stands for a connection that gave no answer at all.
export const stubFetch = (...answers: Answer[]): Stub => {
  const calls: Call[] = [];
  const fetch: typeof globalThis.fetch = async (input, init) => {
    calls.push({
      at: performance.now(),
      url: String(input),
      method: init?.method ?? "GET",
      headers: new Headers(init?.headers),
      body: typeof init?.body === "string" ? init.body : undefined,
      signal: init?.signal,
      credentials: init?.credentials,
    });
    const answer = answers[calls.length - 1];
    if (answer === undefined) throw new Error(`the stub has no answer for call ${calls.length}`);
    if (answer === HANG) return hang(init?.signal ?? undefined);
    if (answer instanceof Error) throw answer;
    return answer;
  };
  return { calls, fetch };
};

// hang is the promise a hung request returns. A call with no signal at all
// never settles, which is the point: nothing but a deadline ends it.
const hang = (signal: AbortSignal | undefined): Promise<Response> =>
  new Promise<Response>((_, reject) => {
    if (signal === undefined) return;
    if (signal.aborted) {
      reject(signal.reason as Error);
      return;
    }
    signal.addEventListener("abort", () => reject(signal.reason as Error), { once: true });
  });

export const json = (status: number, body: unknown): Response =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

export const problem = (status: number, code: string, extra: Record<string, unknown> = {}): Response =>
  new Response(
    JSON.stringify({
      type: "about:blank",
      status,
      code,
      title: "refused",
      detail: `the server refused with ${code}`,
      params: {},
      retryable: false,
      trace_id: "trace-1",
      ...extra,
    }),
    { status, headers: { "content-type": "application/problem+json" } },
  );

export const empty = (status: number): Response => new Response(null, { status });

// An event stream the test writes into after handing the response over.
// push() delivers one chunk as the socket would, end() closes it the way a
// server that went away does, and fail() errors it the way a dropped socket
// does. Chunks given up front are queued before the reader's first read.
export interface Stream {
  readonly response: Response;
  push(chunk: string): void;
  end(): void;
  fail(reason: unknown): void;
}

export const stream = (...chunks: string[]): Stream => {
  const encoder = new TextEncoder();
  let control: ReadableStreamDefaultController<Uint8Array> | undefined;
  let open = true;
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      control = controller;
      for (const chunk of chunks) controller.enqueue(encoder.encode(chunk));
    },
    cancel() {
      open = false;
    },
  });
  const response = new Response(body, { status: 200, headers: { "content-type": "text/event-stream" } });
  return {
    response,
    push: (chunk) => {
      if (open) control?.enqueue(encoder.encode(chunk));
    },
    end: () => {
      if (open) control?.close();
      open = false;
    },
    fail: (reason) => {
      if (open) control?.error(reason);
      open = false;
    },
  };
};
