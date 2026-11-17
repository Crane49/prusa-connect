// A module's locale file, read from where its bundle is served.
//
// A module ships `locales/<lang>.json` beside its entry module, and the host
// merges it under the module's namespace (frontend-i18n.md rule 7). The file
// is a static asset of the module's own bundle, not a route of the core, so
// the SDK has no operation that answers it and `fetch` is what reads it. That
// is the one exception to the transport rule in frontend-structure.md, and it
// is confined to this file.
//
// Absence is not a fault. A module with no catalogue is a module whose author
// has not translated it yet, and it draws the English its code carries.

/**
 * Reads one catalogue, or answers undefined when there is none. The signal is
 * the run's: a reset or a reload stops the read.
 */
export type CatalogueReader = (
  url: string,
  signal?: AbortSignal,
) => Promise<Record<string, string> | undefined>;

/**
 * How long one catalogue read may take. The loader admits modules one at a
 * time, so a read that never answers would keep every later module off the
 * page. A catalogue is a small file beside the bundle, so five seconds is
 * long enough on a slow board.
 */
export const CATALOGUE_TIMEOUT_MS = 5000;

const isMessages = (parsed: unknown): parsed is Record<string, string> => {
  if (typeof parsed !== 'object' || parsed === null) return false;

  return Object.values(parsed).every((value) => typeof value === 'string');
};

/**
 * Reads one catalogue within a deadline. A status that is not 200, a body
 * that is not an object of strings, a request that never arrived, one that
 * ran out of time and one the signal stopped are all "no catalogue".
 */
export const readCatalogueWithin = async (
  url: string,
  signal: AbortSignal | undefined,
  timeoutMs: number,
): Promise<Record<string, string> | undefined> => {
  const deadline = AbortSignal.timeout(timeoutMs);
  try {
    // A module's static asset, served beside its bundle. The core is still
    // reached only through the SDK; the note at the top of this file carries
    // why this one call is not that.
    // eslint-disable-next-line no-restricted-globals
    const answer = await fetch(url, {
      headers: { accept: 'application/json' },
      signal:
        signal === undefined ? deadline : AbortSignal.any([signal, deadline]),
    });
    if (!answer.ok) return undefined;

    const parsed: unknown = await answer.json();

    return isMessages(parsed) ? parsed : undefined;
  } catch {
    return undefined;
  }
};

/** The default reader: one catalogue, within `CATALOGUE_TIMEOUT_MS`. */
export const readCatalogue: CatalogueReader = (url, signal) =>
  readCatalogueWithin(url, signal, CATALOGUE_TIMEOUT_MS);

/**
 * Where a module's catalogue for one language is. The manifest's `ui` block
 * may name a folder; `locales/` beside the entry is the default, and it is
 * what `pnpm run build` in a module's `ui/` folder writes.
 */
export const catalogueUrl = (
  entryUrl: string,
  language: string,
  folder = 'locales',
): string => {
  // The listing carries the entry as a path on this server, so the page's own
  // address is the base. A listing that carries a whole URL is used as it is.
  // The address is read defensively: this module is compiled for a browser and
  // is also loaded by a test that runs under Node.
  const page = (globalThis as { location?: { href?: string } }).location?.href;
  const base = new URL(entryUrl, page ?? 'http://module/');

  return new URL(`${folder}/${language}.json`, base).toString();
};
