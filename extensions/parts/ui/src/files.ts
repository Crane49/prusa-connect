// The service layer for what the core itself holds: the storage the files are
// uploaded to, what the person may do, and the print.
//
// Every call is an operation of `@prusa/connect` on the client the host
// handed over, in session mode. The module's own process never sees a file:
// the person uploads it through the core's storage routes, which take
// write_storage for that person, and the module only records where it went.
import type { PrusaConnect } from '@prusa/connect';
import {
  type PrintDetails,
  type ServerStorage,
  startPrint,
  thumbnailUrlOf,
} from '@prusa/connect-ui/data';
import type { PrintOutcome, PrintRequest } from '@prusa/connect-ui/domain';
import type { ModuleHost } from '@prusa/connect-ui/slots';

/** What the person may do, as the core answered it. */
export interface Allowed {
  /** Read the catalogue: view_resource on this module's kind. */
  readonly read: boolean;
  /** Change the catalogue: operate_resource on this module's kind. */
  readonly write: boolean;
  /** Upload and delete a file: write_storage on the storage kind. */
  readonly upload: boolean;
  /** See a picture, download a file, record a file: read_storage. */
  readonly files: boolean;
}

/** What the stores need of the core. A test hands in a fake. */
export interface FilesService {
  /** The storages this person may see, with what each one allows. */
  storages(signal?: AbortSignal): Promise<readonly ServerStorage[]>;
  /** What the person may do to the catalogue and to its files. */
  allowed(signal?: AbortSignal): Promise<Allowed>;
  /** Refuses a file over the server's upload limit, before anything is made for it. */
  checkSize(body: Blob): Promise<void>;
  /** Writes one file, and answers the length the storage wrote. */
  upload(storage: string, path: string, body: Blob): Promise<number>;
  /** Removes one file, or one folder with everything in it. */
  remove(storage: string, path: string): Promise<void>;
  /** What the slicer wrote into one print file, for the printer picker. */
  describe(
    storage: string,
    path: string,
    signal?: AbortSignal,
  ): Promise<PrintDetails | undefined>;
  /** Sends one file to a printer. */
  print(request: PrintRequest): Promise<PrintOutcome>;
  /** The address of the picture inside a print file, for an `img`. */
  thumbnailUrl(storage: string, path: string): string;
  /** The address of a file's bytes, for an `img` or a download link. */
  contentUrl(storage: string, path: string): string;
}

/** The kind this module's instance is declared under, in module.yaml. */
const MODULE_KIND = 'sink';

/** The kind every storage is declared under. */
const STORAGE_KIND = 'storage';

/**
 * Asks the core whether the person may do one action on one kind. The answer
 * is a hint: the core decides again when the action is attempted, so a page
 * that is wrong here shows a control that is then refused, and the refusal is
 * drawn by its code.
 */
const may = async (
  client: PrusaConnect,
  kind: string,
  action: string,
  signal: AbortSignal,
): Promise<boolean> =>
  (
    await client.checkAccess(
      { body: { object_type: 'resource_kind', object_id: kind, action } },
      signal,
    )
  ).allowed;

/**
 * The code the core answers a file over its upload bound with. It is written
 * here, not imported: the host serves `@prusa/connect-ui`, and a released core
 * that predates the code exports no such name, so the import would stop the
 * whole bundle from loading.
 */
const UPLOAD_TOO_LARGE = 'core.validation/UPLOAD_TOO_LARGE';

/**
 * The refusal the core answers a file over its upload bound with, thrown
 * before the request: a browser that is sending when the core refuses often
 * reports a dropped connection, not the refusal.
 */
const tooLarge = (limit: number): Error =>
  Object.assign(new Error('file over the upload limit'), {
    code: UPLOAD_TOO_LARGE,
    params: { limit_bytes: limit },
  });

/** The core's storage and print routes, on the client the host handed over. */
export const filesOn = (host: ModuleHost): FilesService => {
  const client = host.client;
  const ended = (signal?: AbortSignal): AbortSignal =>
    signal === undefined ? host.signal : AbortSignal.any([signal, host.signal]);
  // Read once. A core that does not say leaves the file to the core. A read
  // that failed is not kept, so the next upload asks again.
  let uploadLimit: Promise<number | undefined> | undefined;
  const limit = (): Promise<number | undefined> =>
    (uploadLimit ??= client.getUiSettings({}, host.signal).then(
      (settings) => settings.max_upload_bytes,
      () => {
        uploadLimit = undefined;

        return undefined;
      },
    ));
  const checkSize = async (body: Blob): Promise<void> => {
    const largest = await limit();
    if (largest !== undefined && body.size > largest) throw tooLarge(largest);
  };

  return {
    storages: async (signal) =>
      (await client.listStorages({}, ended(signal))).items ?? [],
    allowed: async (signal) => {
      const until = ended(signal);
      const [read, write, upload, files] = await Promise.all([
        may(client, MODULE_KIND, 'view_resource', until),
        may(client, MODULE_KIND, 'operate_resource', until),
        may(client, STORAGE_KIND, 'write_storage', until),
        may(client, STORAGE_KIND, 'read_storage', until),
      ]);

      return { read, write, upload, files };
    },
    checkSize,
    upload: async (storage, path, body) => {
      await checkSize(body);

      return (
        await client.uploadStorageObject(
          { path: { storage }, query: { path }, body },
          ended(),
        )
      ).bytes;
    },
    remove: async (storage, path) => {
      await client.deleteStorageEntry(
        { path: { storage }, query: { path } },
        ended(),
      );
    },
    describe: async (storage, path, signal) =>
      (
        await client.statStorageEntry(
          { path: { storage }, query: { path } },
          ended(signal),
        )
      ).print ?? undefined,
    print: async (request) => await startPrint(client, request),
    thumbnailUrl: (storage, path) => thumbnailUrlOf(client, storage, path),
    contentUrl: (storage, path) =>
      `${client.url}/api/v1/storages/${encodeURIComponent(storage)}/content` +
      `?path=${encodeURIComponent(path)}`,
  };
};
