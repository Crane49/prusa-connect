// The requests the Diagnostics room makes. A service names the SDK operation
// and its parameters and holds no state (svelte-data.md).
import type { PrusaConnect } from '@prusa/connect';

export type DiagnosticsClient = Pick<PrusaConnect, 'createDiagnosticsBundle'>;

export type DiagnosticsExtensionsClient = Pick<PrusaConnect, 'getDiagnostics'>;

/** What the admin chose to include beyond the server's own files. */
export interface DiagnosticsChoice {
  /** What module processes wrote to their error output. */
  readonly modules: boolean;
  /** The files installed extensions offer, such as a printer's log lines. */
  readonly extensions: boolean;
}

const FALLBACK_NAME = 'pcl-diagnostics.zip';

/** Asks the server for a redacted bundle that carries the browser's own error buffer. */
export const downloadDiagnostics = async (
  client: DiagnosticsClient,
  browser: readonly unknown[],
  choice: DiagnosticsChoice,
): Promise<{ blob: Blob; name: string }> => {
  const response = await client.createDiagnosticsBundle({
    query: { modules: choice.modules, extensions: choice.extensions },
    // The schema types an entry as an empty object; the server takes any.
    body: [...browser] as Record<string, never>[],
  });
  const name =
    /filename="([^"]+)"/u.exec(
      response.headers.get('content-disposition') ?? '',
    )?.[1] ?? FALLBACK_NAME;

  return { blob: await response.blob(), name };
};

/** The title of each running extension that adds files to a bundle (ADR-20261019-nrq). */
export const readDiagnosticsExtensions = async (
  client: DiagnosticsExtensionsClient,
  signal?: AbortSignal,
): Promise<string[]> =>
  (await client.getDiagnostics({}, signal)).extensions ?? [];
