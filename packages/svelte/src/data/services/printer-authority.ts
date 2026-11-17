// The request the add-printer page makes for the certificate a printer loads
// from its USB drive. A service names the SDK operation and holds no state
// (svelte-data.md).
import type { PrusaConnect } from '@prusa/connect';

export type PrinterAuthorityClient = Pick<PrusaConnect, 'getPrinterAuthority'>;

const NOT_FOUND = 404;

/** The DER certificate of this server's own authority, or null when it has made none. */
export const readPrinterAuthority = async (
  client: PrinterAuthorityClient,
  signal?: AbortSignal,
): Promise<Blob | null> => {
  try {
    return await (await client.getPrinterAuthority({}, signal)).blob();
  } catch (thrown) {
    if ((thrown as { status?: unknown } | null)?.status === NOT_FOUND) {
      return null;
    }
    throw thrown;
  }
};
