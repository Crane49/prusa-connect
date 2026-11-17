// The namespace the host serves this bundle under, read from the bundle's own
// address as the library's style injection reads it. A dialog or a popover is
// portalled out of the module's slot, and receives this module's utilities only
// when it carries this as its own `data-module` (ADR-20261029-zzg rule 6).
// Served from anywhere else, such as in a test, it is empty, which the
// unnarrowed scope `[data-module]` still matches.
export const OWN_MODULE = decodeURIComponent(
  /\/modules\/([^/]+)\/ui\//u.exec(import.meta.url)?.[1] ?? '',
);
