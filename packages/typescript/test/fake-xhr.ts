// An XMLHttpRequest that answers from the test and records what it was asked,
// so a test reads the request the transport built and never opens a socket.

interface ProgressEventLike {
  readonly loaded: number;
  readonly total: number;
}

export class FakeXhr {
  static sent: FakeXhr[] = [];

  method = "";
  url = "";
  readonly headers = new Headers();
  body: unknown;
  withCredentials = false;
  responseType = "";
  status = 0;
  statusText = "";
  response: unknown = null;
  readonly upload: { onprogress: ((event: ProgressEventLike) => void) | null } = { onprogress: null };
  onload: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onabort: (() => void) | null = null;
  #headers = "";

  open(method: string, url: string): void {
    this.method = method;
    this.url = url;
  }

  setRequestHeader(name: string, value: string): void {
    this.headers.set(name, value);
  }

  send(body: unknown): void {
    this.body = body;
    FakeXhr.sent.push(this);
  }

  abort(): void {
    this.onabort?.();
  }

  getAllResponseHeaders(): string {
    return this.#headers;
  }

  progress(loaded: number, total: number): void {
    this.upload.onprogress?.({ loaded, total });
  }

  answer(status: number, body: string, headers: Record<string, string> = {}): void {
    this.status = status;
    this.response = new Blob([body]);
    this.#headers = Object.entries(headers)
      .map(([name, value]) => `${name}: ${value}\r\n`)
      .join("");
    this.onload?.();
  }

  fail(): void {
    this.onerror?.();
  }
}

// install puts the fake where the transport looks for XMLHttpRequest and
// answers a function that puts the real one back.
export const install = (): (() => void) => {
  const held = (globalThis as { XMLHttpRequest?: unknown }).XMLHttpRequest;
  FakeXhr.sent = [];
  (globalThis as { XMLHttpRequest?: unknown }).XMLHttpRequest = FakeXhr;

  return () => {
    (globalThis as { XMLHttpRequest?: unknown }).XMLHttpRequest = held;
  };
};
