// A fetch for the one kind of request whose progress a page draws: a file on
// its way up.
//
// fetch reports nothing while a request body goes up. A streamed body would
// let the page count the bytes, but a browser streams one only over HTTP/2,
// and a core on a Raspberry Pi serves HTTP/1.1. XMLHttpRequest reports
// `upload.onprogress` on every browser, so this request goes over it. The
// answer is built into a Response, so the transport reads it the way it reads
// every other one.

/**
 * Called as a request body goes up, with the bytes sent and the body's
 * length. `total` is 0 when the browser cannot tell the body's length.
 */
export type UploadProgress = (sent: number, total: number) => void;

// headersOf reads the block getAllResponseHeaders answers, one
// `name: value` line per header.
const headersOf = (block: string): Headers => {
  const headers = new Headers();
  for (const line of block.trim().split(/[\r\n]+/)) {
    const at = line.indexOf(":");
    if (at > 0) headers.append(line.slice(0, at).trim(), line.slice(at + 1).trim());
  }

  return headers;
};

// A status that carries no body. Response refuses one built with a body.
const BODYLESS = new Set([204, 205, 304]);

export const withProgress =
  (onProgress: UploadProgress): typeof fetch =>
  (input, init) =>
    new Promise<Response>((resolve, reject) => {
      const signal = init?.signal ?? undefined;
      if (signal?.aborted === true) {
        reject(signal.reason);
        return;
      }

      const xhr = new XMLHttpRequest();
      const abort = (): void => {
        xhr.abort();
      };
      const settle = (): void => {
        signal?.removeEventListener("abort", abort);
      };

      xhr.open(init?.method ?? "GET", String(input));
      xhr.responseType = "blob";
      // fetch's "same-origin", which is what the transport asks for, is what
      // XMLHttpRequest does with withCredentials left false.
      xhr.withCredentials = init?.credentials === "include";
      new Headers(init?.headers).forEach((value, name) => {
        xhr.setRequestHeader(name, value);
      });

      xhr.upload.onprogress = (event) => {
        onProgress(event.loaded, event.total);
      };
      xhr.onload = () => {
        settle();
        resolve(
          new Response(BODYLESS.has(xhr.status) ? null : (xhr.response as Blob), {
            status: xhr.status,
            statusText: xhr.statusText,
            headers: headersOf(xhr.getAllResponseHeaders()),
          }),
        );
      };
      // The same error fetch rejects with when no answer came, so the
      // transport wraps it in a NetworkError the same way.
      xhr.onerror = () => {
        settle();
        reject(new TypeError("the request failed"));
      };
      xhr.onabort = () => {
        settle();
        reject(signal?.reason ?? new DOMException("the request was aborted", "AbortError"));
      };
      signal?.addEventListener("abort", abort, { once: true });

      xhr.send((init?.body ?? null) as XMLHttpRequestBodyInit | null);
    });
