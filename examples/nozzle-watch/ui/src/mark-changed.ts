// The module's one device-action: registered as `mark-changed`, it stamps
// today's date for this printer, and `nozzle-tab` reads the same record back.
//
// `deviceId` is the only input a device-action receives (ADR-20260911-4p6 rule 10),
// so a printer this page never loaded has nothing here to act on. The store
// takes one key per module (module-store.svelte.ts: a key is one DNS-1123
// label, and a device's id is not one), so every printer's mark lives under
// the one key `changed`, as the JSON record a caller of a `ScopedStore`
// writes when what it holds is a structure rather than one string.
import type { ModuleHost } from '@prusa/connect-ui/slots';

const KEY = 'changed';

const marksOf = (host: ModuleHost): Record<string, string> => {
  const raw = host.store.get(KEY);
  if (raw === undefined) return {};

  try {
    const parsed: unknown = JSON.parse(raw);

    return typeof parsed === 'object' && parsed !== null
      ? (parsed as Record<string, string>)
      : {};
  } catch {
    // A record nothing here wrote. The tab reads it the same as an empty one.
    return {};
  }
};

/** The date `mark-changed` last wrote for one printer, read by `nozzle-tab`. */
export const changedAt = (
  host: ModuleHost,
  deviceId: string,
): string | undefined => marksOf(host)[deviceId];

/** Builds the run function `index.ts` registers under `mark-changed`. */
export const markChanged =
  (host: ModuleHost) =>
  (deviceId: string): Promise<void> => {
    if (host.fleet.device(deviceId) === undefined) {
      return Promise.reject(new Error(host.t('nozzle.absent')));
    }

    const marks = marksOf(host);
    marks[deviceId] = new Date().toISOString();
    host.store.set(KEY, JSON.stringify(marks));

    return Promise.resolve();
  };
