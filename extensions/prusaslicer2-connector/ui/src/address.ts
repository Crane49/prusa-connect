// The address a person types into PrusaSlicer for one printer.
//
// The module's routes are mounted under /api/v1/modules/<namespace>/. A
// bundle does not know the namespace the operator installed it under, but the
// host writes it into the address of the module's own pages, /m/<namespace>/.
// The printer's set of PrusaLink routes is printers/<id>/ under the mount, and
// the address ends in a slash so PrusaSlicer adds api/version to it as it is.

/** The namespace out of a page address the host wrote, /m/<namespace>/<path>. */
export const namespaceOf = (pageHref: string): string | undefined => {
  const match = /^\/m\/([^/]+)\//.exec(pageHref);

  return match?.[1];
};

/** The address of one printer's PrusaLink routes on this server. */
export const printerAddress = (
  origin: string,
  namespace: string,
  printerId: string,
): string =>
  `${origin.replace(/\/+$/, '')}/api/v1/modules/${namespace}/printers/${encodeURIComponent(printerId)}/`;
