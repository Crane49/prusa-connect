/**
 * Where a module's page is addressed: `/m/<namespace>/<path>` (ADR-20260911-4p6
 * rule 9).
 *
 * It is a leaf of its own, beside `version.ts`, because the store that reads
 * the listing and the registry that holds a loaded module both build the same
 * address, and neither may import the other.
 */
export const modulePageHref = (namespace: string, path: string): string =>
  `/m/${namespace}/${path}`;
