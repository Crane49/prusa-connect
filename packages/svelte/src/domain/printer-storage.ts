import type { components } from '@prusa/connect';

/** One storage device a printer holds: a USB stick, its own flash. */
export type DeviceStorage = components['schemas']['DeviceStorage'];

/** What a printer said its storages are, as `list-device-storages` answers. */
export type DeviceStorages = components['schemas']['DeviceStorages'];

/**
 * Why a file cannot be put on a printer from here, before anything is sent.
 *
 * - `noStorage`: the printer answered and has nowhere to write, which on a
 *   Prusa printer means no USB drive is in it
 * - `storagesUnknown`: the printer has not said what storages it holds yet
 * - `noServerStorage`: this server has no storage to hold the file on its way
 *   to the printer
 */
export type UploadBlock = 'noStorage' | 'storagesUnknown' | 'noServerStorage';

/**
 * The storage a file lands on: the first one that takes a write, which is the
 * order the firmware lists them in.
 */
export const writableStorageOf = (
  storages: readonly DeviceStorage[],
): DeviceStorage | undefined => storages.find((storage) => !storage.read_only);

/** What a page has read of one printer's storages so far. */
export interface PrinterStoragesRead {
  /** The newest answer, absent until one came. */
  readonly storages?: DeviceStorages | undefined;
  /** Why the newest read failed, absent when it did not. */
  readonly error?: Error | undefined;
}

/**
 * HTTP statuses that say the read can never work for this person: they may not
 * operate the printer, or it is not there. Neither is helped by asking again,
 * and a person who may not operate a printer cannot upload to it anyway.
 */
const REFUSED = new Set([403, 404]);

const refused = (error: Error): boolean => {
  // The narrowing cast reads one field off a value the SDK threw, which is a
  // trust boundary (typescript.md).
  const status = (error as { status?: unknown }).status;

  return typeof status === 'number' && REFUSED.has(status);
};

/**
 * Why an upload to one printer cannot start, or undefined when it can or
 * nothing is known yet.
 *
 * An answer decides when there is one, even beside a failed ask again. A read
 * that failed for any other reason than a refusal, a printer slow to answer or
 * a full command queue, is `storagesUnknown`, whose sentence offers to ask
 * again; leaving the place empty would hide the upload without a word. The
 * first read still on its way says nothing, because the printer is asked for
 * a few seconds at most and a page that opens on a ready printer should not
 * flash a warning first.
 */
export const uploadBlockOf = (
  printer: PrinterStoragesRead,
  serverStorage: string | undefined,
): UploadBlock | undefined => {
  if (serverStorage === undefined) return 'noServerStorage';

  const { storages, error } = printer;
  if (storages === undefined) {
    if (error === undefined || refused(error)) return undefined;

    return 'storagesUnknown';
  }

  if (!storages.reported) return 'storagesUnknown';
  if (writableStorageOf(storages.items ?? []) === undefined) return 'noStorage';

  return undefined;
};
