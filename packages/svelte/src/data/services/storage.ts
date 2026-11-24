// The requests the print sheet makes about this server's own storage. A
// service names the SDK operation and its parameters and keeps no state
// (svelte-data.md).
import type { PrusaConnect } from '@prusa/connect';

/** The storages this server declares, as `list-storages` answers. */
export type StoragePage = Awaited<ReturnType<PrusaConnect['listStorages']>>;

/** One storage, with what it allows: browse, write, move, ranges. */
export type ServerStorage = NonNullable<StoragePage['items']>[number];

/** One directory of a storage, as `list-storage-files` answers. */
export type StorageListing = Awaited<
  ReturnType<PrusaConnect['listStorageFiles']>
>;

/** One file or folder in that directory. */
export type StorageEntry = NonNullable<StorageListing['items']>[number];

/** One file described on its own, as `stat-storage-entry` answers. */
export type StoredFile = Awaited<ReturnType<PrusaConnect['statStorageEntry']>>;

/**
 * What the slicer wrote into one file: the print time, the filament, the layer
 * height, the nozzle, the material, the printer model, and the size of the
 * picture the thumbnail route serves. Every field is optional, and an absent
 * one means the file did not say.
 */
export type PrintDetails = NonNullable<StoredFile['print']>;

/** What `upload-storage-object` answers: the path, the length and the digest. */
export type SavedObject = Awaited<
  ReturnType<PrusaConnect['uploadStorageObject']>
>;

/**
 * The default of `core.api.max_upload_size`, in bytes. A server may be set
 * higher or lower; `max_upload_bytes` in its settings is the number it runs
 * with.
 */
export const UPLOAD_LIMIT_BYTES = 200 * 1024 * 1024;

/** The code the core answers a body over an endpoint's bound with. */
export const BODY_TOO_LARGE = 'core.validation/BODY_TOO_LARGE';

/** The code the core answers a file over `max_upload_bytes` with. */
export const UPLOAD_TOO_LARGE = 'core.validation/UPLOAD_TOO_LARGE';

/** What the storage store needs of the SDK, and no more. */
export type StorageClient = Pick<
  PrusaConnect,
  | 'listStorages'
  | 'listStorageFiles'
  | 'statStorageEntry'
  | 'uploadStorageObject'
  | 'deleteStorageEntry'
  | 'moveStorageEntry'
  | 'createStorageFolder'
> &
  Pick<PrusaConnect, 'url'>;

/** Every storage this caller may see. */
export const readStorages = (client: StorageClient): Promise<StoragePage> =>
  client.listStorages();

/**
 * One directory inside one storage. An empty path is the storage's root.
 *
 * The signal is the store's, so a listing a person has already clicked past is
 * stopped rather than left to answer into a screen showing another folder
 * (svelte-data.md).
 */
export const readStorageFiles = (
  client: StorageClient,
  storage: string,
  path: string,
  signal?: AbortSignal,
): Promise<StorageListing> =>
  client.listStorageFiles(
    {
      path: { storage },
      query: path === '' ? {} : { path },
    },
    signal,
  );

/**
 * Writes one file. The body goes up as `application/octet-stream`, so a `Blob`
 * or a `File` from a drop zone is handed over as it is. The browser reads the
 * file off the disk as it sends it, so a four gigabyte file costs no four
 * gigabytes of memory in the page.
 *
 * `onProgress` is called with the bytes sent so far as they go up.
 */
export const uploadObject = (
  client: StorageClient,
  storage: string,
  path: string,
  body: Blob,
  onProgress?: (sent: number, total: number) => void,
  signal?: AbortSignal,
): Promise<SavedObject> =>
  client.uploadStorageObject(
    {
      path: { storage },
      query: { path },
      body,
      ...(onProgress === undefined ? {} : { onProgress }),
    },
    signal,
  );

/**
 * The folder at a storage's root that holds the temporary copies. The core
 * names it, and lists none of it with the storage's files.
 */
const TEMPORARY_FOLDER = '.pcl-incoming/';

/**
 * Whether a path in a storage is a temporary copy. Such a file is deleted
 * once it is sent, so nothing is linked to it and no picture is read from it
 * afterwards.
 */
export const isTemporarySource = (path: string): boolean =>
  path.startsWith(TEMPORARY_FOLDER);

/**
 * Writes one temporary copy: a file on its way to a printer that is not kept.
 * `name` is the file's own name. The server puts the copy in a folder of its
 * own and answers where, and that path is the source a send names. The server
 * deletes the copy once the file has landed on the printer or its transfer was
 * stopped, and an hour after a send of it failed.
 */
export const uploadTemporaryObject = (
  client: StorageClient,
  storage: string,
  name: string,
  body: Blob,
  onProgress?: (sent: number, total: number) => void,
  signal?: AbortSignal,
): Promise<SavedObject> =>
  client.uploadStorageObject(
    {
      path: { storage },
      query: { path: name, temporary: true },
      body,
      ...(onProgress === undefined ? {} : { onProgress }),
    },
    signal,
  );

/**
 * Describes one file: its length, its digest, and what the slicer wrote into
 * it. It opens the file on the server, so a screen asks for the files it is
 * about to show and not for a whole listing.
 */
export const readStorageEntry = (
  client: StorageClient,
  storage: string,
  path: string,
  signal?: AbortSignal,
): Promise<StoredFile> =>
  client.statStorageEntry({ path: { storage }, query: { path } }, signal);

/**
 * The address of the picture stored inside one file, for an `img` tag.
 *
 * The route takes the same permission as reading the file. A page the core
 * serves sends its session cookie with the image request, so the tag works as
 * it is. A caller holding a bearer token instead has to fetch the picture
 * itself and hand the tag an object URL, because an `img` tag sends no
 * Authorization header.
 */
export const thumbnailUrlOf = (
  client: StorageClient,
  storage: string,
  path: string,
): string =>
  `${client.url}/api/v1/storages/${encodeURIComponent(storage)}/thumbnail` +
  `?path=${encodeURIComponent(path)}`;

/**
 * The address of one file's bytes, for a download link. It takes the same
 * permission as listing the storage, so a page the core serves sends its
 * session cookie with the request and an `a` tag works as it is.
 */
export const contentUrlOf = (
  client: StorageClient,
  storage: string,
  path: string,
): string =>
  `${client.url}/api/v1/storages/${encodeURIComponent(storage)}/content` +
  `?path=${encodeURIComponent(path)}`;

/** Removes one file or one empty folder. */
export const deleteStorageEntry = (
  client: StorageClient,
  storage: string,
  path: string,
): Promise<void> =>
  client.deleteStorageEntry({ path: { storage }, query: { path } });

/**
 * Moves one entry inside one storage. Both paths are relative to that
 * storage's root, and the answer names where the entry ended up.
 */
export const moveStorageEntry = async (
  client: StorageClient,
  storage: string,
  from: string,
  to: string,
): Promise<void> => {
  await client.moveStorageEntry({ path: { storage }, body: { from, to } });
};

/** Creates one folder, and any parent it needs, relative to the storage's root. */
export const createStorageFolder = async (
  client: StorageClient,
  storage: string,
  path: string,
): Promise<void> => {
  await client.createStorageFolder({ path: { storage }, body: { path } });
};
