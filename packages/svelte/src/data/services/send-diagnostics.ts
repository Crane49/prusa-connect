// The upload behind Send to Prusa. The browser posts the bundle the server
// built to the support portal, and the server itself never calls out
// (ADR-20261015-mde rule 16). The portal is not a route of the core, so the SDK has
// no operation for it and `fetch` is what reaches it. That is the second
// exception to the transport rule in frontend-structure.md, beside the module
// catalogue read, and it is confined to this file.

/** The product name the portal files the bundle under. */
export const DIAGNOSTICS_PRODUCT = 'connect-local';

/** Where the portal sends the person to finish, and when the upload expires. */
export interface DiagnosticsUpload {
  url: string;
  expiresAt: string;
}

/**
 * A portal that answered with anything but the upload. `status` is 0 when no
 * answer arrived, such as a network failure or a cross-origin refusal.
 * `code` and `message` are the portal's own, when it sent them.
 * `retryAfter` is in seconds, from a 429.
 */
export class DiagnosticsSendError extends Error {
  readonly status: number;
  readonly code: string | undefined;
  readonly retryAfter: number | undefined;

  constructor(
    status: number,
    code: string | undefined,
    message: string,
    retryAfter?: number,
  ) {
    super(message);
    this.name = 'DiagnosticsSendError';
    this.status = status;
    this.code = code;
    this.retryAfter = retryAfter;
  }
}

/** The request the upload makes, so a test can stand in for the network. */
export type DiagnosticsPost = (
  url: string,
  init: RequestInit,
) => Promise<Response>;

// The global fetch, bound to its global, read when the upload starts so a
// test that stubs it is seen.
// eslint-disable-next-line no-restricted-globals
const globalPost: DiagnosticsPost = (url, init) => fetch(url, init);

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null;

const textOf = (value: unknown): string | undefined =>
  typeof value === 'string' && value !== '' ? value : undefined;

const secondsOf = (header: string | null): number | undefined => {
  if (header === null) return undefined;
  const seconds = Number(header);

  return Number.isFinite(seconds) && seconds > 0 ? seconds : undefined;
};

const sameOrigin = (url: string, target: string): boolean => {
  try {
    return new URL(url).origin === new URL(target).origin;
  } catch {
    return false;
  }
};

/** The address the bundle is posted to. */
export const diagnosticsUploadUrl = (
  target: string,
  version: string,
): string => {
  const url = new URL(`${target.replace(/\/+$/u, '')}/api/diagnostics`);
  url.searchParams.set('product', DIAGNOSTICS_PRODUCT);
  url.searchParams.set('version', version);

  return url.toString();
};

/**
 * Posts one bundle to the portal at `target`. No cookie and no credential
 * goes with it: the person signs in on the portal's own page afterwards.
 */
export const sendDiagnostics = async (
  bundle: Blob,
  target: string,
  version: string,
  post: DiagnosticsPost = globalPost,
): Promise<DiagnosticsUpload> => {
  let response: Response;
  try {
    response = await post(diagnosticsUploadUrl(target, version), {
      method: 'POST',
      headers: { 'Content-Type': 'application/zip' },
      body: bundle,
      credentials: 'omit',
    });
  } catch (thrown) {
    throw new DiagnosticsSendError(
      0,
      undefined,
      thrown instanceof Error ? thrown.message : String(thrown),
    );
  }

  let parsed: unknown;
  try {
    parsed = await response.json();
  } catch {
    parsed = undefined;
  }
  const body = isRecord(parsed) ? parsed : {};

  const url = textOf(body.url);
  if (response.status === 201 && url !== undefined) {
    // The tab is sent to this address, so only a page of the same portal is
    // taken; anything else would let an answer open any address at all.
    if (!sameOrigin(url, target)) {
      throw new DiagnosticsSendError(
        response.status,
        undefined,
        'the portal answered with an address outside itself',
      );
    }

    return { url, expiresAt: textOf(body.expiresAt) ?? '' };
  }

  throw new DiagnosticsSendError(
    response.status,
    textOf(body.error),
    textOf(body.message) ?? response.statusText,
    secondsOf(response.headers.get('retry-after')),
  );
};
