import type { NearbyPrinter } from '../../data';

/**
 * The service name a Prusa Connect server announces besides the printer's
 * own. A Buddy printer announces only `_prusalink._tcp`, so this one tells a
 * server from a printer.
 */
const SERVER_SERVICE = '_prusa-connect._tcp';

/**
 * Every entry that is a printer still to add here. The server names a
 * connected printer in `device_id`, and that printer is already on the page
 * above the panel. Another Prusa Connect server is not a printer to add, so
 * it is left out as well.
 */
export const printersToAdd = (
  entries: readonly NearbyPrinter[],
): NearbyPrinter[] =>
  entries.filter(
    (entry) =>
      entry.device_id === undefined &&
      !(entry.services ?? []).includes(SERVER_SERVICE),
  );
