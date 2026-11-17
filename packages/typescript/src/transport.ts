import { ApiError, isProblem, NetworkError, type Problem } from "./errors.js";
import type { Operation, OperationRequest, Transport } from "./operation.js";
import { withProgress } from "./progress.js";

/**
 * A token provider is called before every request that needs a credential,
 * so a caller that refreshes tokens hands the current one over each time.
 */
export type TokenProvider = () => string | Promise<string>;

/** A bearer credential: the token the core minted, or a function that returns the current one. */
export type BearerAuth = string | TokenProvider;

/**
 * The browser's own session. A page the core serves signs in through the
 * login ceremony and holds no token: the credential is the HttpOnly cookie
 * the login set, and the browser attaches it to a same-origin request on its
 * own. In this mode no header carries a credential, and every fetch, the
 * event stream included, asks the browser for the cookie with
 * `credentials: "same-origin"`.
 */
export interface SessionAuth {
  readonly kind: "session";
}

/** The credential: a bearer token, a function that returns the current one, or the browser's session. */
export type Auth = BearerAuth | SessionAuth;

/** What an HttpTransport is built from. */
export interface TransportOptions {
  // The core's origin, such as `https://printers.local:8080`. A path after the
  // origin is kept, so a core behind a prefix works too. Omitted, every
  // request goes to a relative path, which the browser resolves against the
  // page's own origin: the default for a page the core serves.
  readonly url?: string;
  readonly auth: Auth;
  // The fetch to use. Defaults to the global one, and a test passes a stub.
  readonly fetch?: typeof fetch;
  // How many times a request whose connection failed is sent again.
  readonly retries?: number;
  // The wait before the first retry, in milliseconds. Each retry doubles it.
  readonly retryDelay?: number;
  // How long one attempt may take before it is given up, in milliseconds.
  // Every retry gets the whole of it again, so a request sent three times may
  // take three of these. Zero waits for as long as the server takes, which is
  // what an operation reading a stream already does.
  readonly timeoutMs?: number;
  // Ends every request this transport sends. A request in flight when it
  // aborts is aborted and never sent again, and a request made after it
  // aborted is refused here without reaching the network. The error is the
  // signal's reason, an AbortError unless the owner gave another.
  readonly signal?: AbortSignal;
  // Called when the core answers 401, before the call throws. A page uses it
  // to learn that its session ended whichever call found out first.
  readonly onUnauthorized?: () => void;
  // Called with every refusal and every network failure, before the call
  // throws, so an application can keep its own record of what failed. A call
  // its caller aborted is not a failure and is not reported.
  readonly onFailure?: (
    failure: ApiError | NetworkError,
    operation: { readonly id: string; readonly method: string },
  ) => void;
}

const DEFAULT_RETRIES = 2;
const DEFAULT_RETRY_DELAY = 200;
// Long enough for a slow printer listing on a Raspberry Pi, short enough that a
// page waiting on a server that went away says so rather than hanging.
const DEFAULT_TIMEOUT = 30_000;

// The methods HTTP defines as safe to repeat. A POST that failed on the wire
// may still have reached the server, so it is never sent twice by this layer.
const IDEMPOTENT: ReadonlySet<string> = new Set(["GET", "HEAD", "PUT", "DELETE", "OPTIONS", "TRACE"]);

/**
 * HttpTransport sends operations over fetch: it builds the URL, attaches the
 * credential, retries a safe request whose connection failed, and turns a
 * refusal into an ApiError.
 */
export class HttpTransport implements Transport {
  readonly url: string;
  private readonly auth: Auth;
  private readonly fetcher: typeof fetch;
  private readonly retries: number;
  private readonly retryDelay: number;
  private readonly timeoutMs: number;
  private readonly ended: AbortSignal | undefined;
  private readonly onUnauthorized: (() => void) | undefined;
  private readonly onFailure: TransportOptions["onFailure"];

  constructor(options: TransportOptions) {
    this.url = (options.url ?? "").replace(/\/+$/, "");
    this.auth = options.auth;
    // The global fetch is called through a closure: a browser refuses it with
    // any other `this`.
    this.fetcher = options.fetch ?? ((input, init) => globalThis.fetch(input, init));
    this.retries = options.retries ?? DEFAULT_RETRIES;
    this.retryDelay = options.retryDelay ?? DEFAULT_RETRY_DELAY;
    this.timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT;
    this.ended = options.signal;
    this.onUnauthorized = options.onUnauthorized;
    this.onFailure = options.onFailure;
  }

  async call<T>(operation: Operation, request: OperationRequest, signal?: AbortSignal): Promise<T> {
    const headers = new Headers();
    for (const [name, value] of Object.entries(request.header ?? {})) {
      if (value !== undefined) headers.set(name, value);
    }

    // A session rides in the browser's cookie and never in a header. The
    // core reads a Bearer header before the cookie, so setting none is what
    // lets the cookie count.
    if (!isSession(this.auth) && !operation.anonymous) {
      headers.set("Authorization", `Bearer ${await this.token(this.auth)}`);
    }

    let body: BodyInit | undefined;
    if (operation.body !== undefined) {
      headers.set("Content-Type", operation.body);
      body = operation.body === "application/json" ? JSON.stringify(request.body) : (request.body as BodyInit);
    }

    const url = this.urlOf(operation, request);
    const init: RequestInit = { method: operation.method, headers };
    // Asked for on every request, an anonymous one too: a browser attaches a
    // same-origin cookie by default in a page, but not from a worker or an
    // extension, and the setting names what the transport relies on.
    if (isSession(this.auth)) init.credentials = "same-origin";
    if (body !== undefined) init.body = body;

    let response: Response;
    try {
      // A request that reports its progress goes over XMLHttpRequest, and it
      // is never sent twice: a retry would send the whole file again, and a
      // bar would jump back to zero.
      const fetcher = request.onProgress === undefined ? this.fetcher : withProgress(request.onProgress);
      response = await this.send(
        fetcher,
        url,
        init,
        request.onProgress === undefined && IDEMPOTENT.has(operation.method) && !(body instanceof ReadableStream),
        signal,
        // An answer handed over as the Response itself is read by the caller
        // after this call returns: the event stream, the two camera routes and a
        // file download. A deadline on the fetch would cut that read short, so
        // those wait on the caller's own signal and on nothing else. So does an
        // operation the document marks long-running, whose answer waits on a
        // machine for as long as the machine takes. So does a request that
        // reports its progress, whose file may take longer to go up than the
        // deadline allows.
        request.onProgress !== undefined || operation.response === "raw" || operation.longRunning === true
          ? 0
          : this.timeoutMs,
      );
    } catch (thrown) {
      if (thrown instanceof NetworkError && signal?.aborted !== true) this.onFailure?.(thrown, operation);
      throw thrown;
    }

    if (response.status === 401) this.onUnauthorized?.();
    if (!response.ok) {
      const refused = await refusal(response);
      this.onFailure?.(refused, operation);
      throw refused;
    }

    switch (operation.response) {
      case "json":
        return (await response.json()) as T;
      case "text":
        return (await response.text()) as T;
      case "raw":
        return response as T;
      case "none":
        return undefined as T;
    }
  }

  // urlOf fills the path template and appends the query. A list is joined
  // with commas, which is how the document declares every list parameter
  // (`explode: false`).
  urlOf(operation: Operation, request: OperationRequest): string {
    const path = operation.path.replace(/\{([^}]+)\}/g, (_, name: string) => {
      const value = request.path?.[name];
      if (value === undefined) throw new TypeError(`${operation.id}: path parameter ${name} is missing`);
      return encodeURIComponent(String(value));
    });

    const query = new URLSearchParams();
    for (const [name, value] of Object.entries(request.query ?? {})) {
      if (value === undefined || value === null) continue;
      query.set(name, Array.isArray(value) ? value.map(String).join(",") : String(value));
    }

    const search = query.toString();
    return this.url + path + (search === "" ? "" : `?${search}`);
  }

  private async token(auth: BearerAuth): Promise<string> {
    return typeof auth === "string" ? auth : await auth();
  }

  // send retries on a failure to get any answer at all and on nothing else.
  // A status, whatever it is, is an answer, and it is the caller's to read.
  //
  // The deadline is per attempt, so a retry is not spending what the attempt
  // before it already spent. The caller's signal is the one that stops the
  // whole thing: an attempt it aborted is never sent again, while an attempt
  // that only ran out of time is.
  private async send(
    fetcher: typeof fetch,
    url: string,
    init: RequestInit,
    repeatable: boolean,
    signal: AbortSignal | undefined,
    timeoutMs: number,
  ): Promise<Response> {
    const attempts = repeatable ? this.retries + 1 : 1;
    const ended = this.ended;
    if (ended?.aborted === true) throw reasonOf(ended);
    const caller = combine(signal, ended);

    for (let attempt = 1; ; attempt += 1) {
      const deadline = timeoutMs > 0 ? AbortSignal.timeout(timeoutMs) : undefined;
      const attemptSignal = combine(caller, deadline);

      try {
        return await fetcher(url, attemptSignal === undefined ? init : { ...init, signal: attemptSignal });
      } catch (cause) {
        // Read again: the signal may have aborted while the attempt was out.
        if (ended !== undefined && isAborted(ended)) throw reasonOf(ended);
        if (signal?.aborted === true || attempt >= attempts) throw new NetworkError(cause, attempt);
        await wait(this.retryDelay * 2 ** (attempt - 1), signal);
      }
    }
  }
}

// combine is the signal one attempt is sent under: the caller's, the
// attempt's deadline, or both together.
//
// AbortSignal.any is what joins the two. It has been in every runtime this
// package supports since 2024, and the fallback below is for an older one: a
// controller this code aborts from whichever signal fires first. The listeners
// it adds live as long as the caller's signal does, which is one request.
export const combine = (caller: AbortSignal | undefined, deadline: AbortSignal | undefined): AbortSignal | undefined => {
  if (caller === undefined) return deadline;
  if (deadline === undefined) return caller;
  if (typeof AbortSignal.any === "function") return AbortSignal.any([caller, deadline]);

  const controller = new AbortController();
  const stop = (signal: AbortSignal) => (): void => controller.abort(signal.reason);
  for (const signal of [caller, deadline]) {
    if (signal.aborted) {
      controller.abort(signal.reason);
      break;
    }
    signal.addEventListener("abort", stop(signal), { once: true });
  }

  return controller.signal;
};

// isAborted reads the flag through a call, so the compiler does not keep the
// answer it narrowed before an await.
const isAborted = (signal: AbortSignal): boolean => signal.aborted;

// reasonOf is the error a request ended by the transport's own signal throws.
const reasonOf = (signal: AbortSignal): Error =>
  signal.reason instanceof Error ? signal.reason : new DOMException("the client was ended", "AbortError");

// isSession tells the session from a bearer credential, which is a string or
// a function and never an object.
const isSession = (auth: Auth): auth is SessionAuth => typeof auth === "object";

// refusal reads the body of a failed response once and turns it into the
// error every caller sees. A problem document fills the code; anything else,
// such as a proxy's HTML, is kept as text so nothing is lost.
const refusal = async (response: Response): Promise<ApiError> => {
  const text = await response.text();
  let body: Problem | string | undefined = text === "" ? undefined : text;

  const type = response.headers.get("content-type") ?? "";
  if (type.includes("json") && text !== "") {
    try {
      const decoded: unknown = JSON.parse(text);
      if (isProblem(decoded)) body = decoded;
    } catch {
      // Not JSON after all. The text stands.
    }
  }

  return new ApiError(response.status, body);
};

// wait resolves after ms, and rejects with the signal's reason when it aborts
// first. The stream reader shares it for its own backoff.
export const wait = (ms: number, signal: AbortSignal | undefined): Promise<void> =>
  new Promise((resolve, reject) => {
    if (ms <= 0) {
      resolve();
      return;
    }
    const timer = setTimeout(() => {
      signal?.removeEventListener("abort", abort);
      resolve();
    }, ms);
    const abort = (): void => {
      clearTimeout(timer);
      reject(signal?.reason instanceof Error ? signal.reason : new Error("aborted"));
    };
    signal?.addEventListener("abort", abort, { once: true });
  });
