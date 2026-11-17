// The dynamic import, in a plain module of its own.
//
// It is not in `loader.svelte.ts` because Svelte's module compiler drops
// comments, and the `@vite-ignore` below has to survive into the bundler's
// input. Without it every build of a consumer warns that the import cannot be
// analysed, which is true and is the point: the URL is the core's, read from
// the resources listing at runtime.

/** How a bundle is fetched. A test hands the loader a stub instead. */
export type ModuleImport = (url: string) => Promise<unknown>;

/**
 * Imports a module's entry. The URL comes from the core's listing and never
 * from the bundle, so a module cannot point the host at an origin of its own
 * (ADR-20260911-4p6 rule 15).
 */
export const dynamicImport: ModuleImport = (url) =>
  import(/* @vite-ignore */ url) as Promise<unknown>;
