// Whether the page is shown embedded: a module page opened with `?embed=1`
// draws without the sidebar and the header (ADR-20261112-2eb rule 6).
// PrusaSlicer opens a module page that way in its own window. The page still
// needs the person signed in, as every page does.
import { PATHS } from './routes';

/** The query parameter that asks for the embedded page. */
export const EMBED_PARAM = 'embed';

/** True for a module page whose address carries `embed=1`. */
export const isEmbedded = (
  pattern: string | undefined,
  search: string,
): boolean =>
  pattern === PATHS.modulePage &&
  new URLSearchParams(search).get(EMBED_PARAM) === '1';
