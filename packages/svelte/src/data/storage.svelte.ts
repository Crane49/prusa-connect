// This server's own storages and their files, for the print sheet.
import { type DataFault, faultOf } from './fault';
import {
  BODY_TOO_LARGE,
  contentUrlOf,
  createStorageFolder,
  deleteStorageEntry,
  moveStorageEntry,
  type PrintDetails,
  readStorageEntry,
  readStorageFiles,
  readStorages,
  type SavedObject,
  type ServerStorage,
  type StorageClient,
  type StorageEntry,
  thumbnailUrlOf,
  UPLOAD_LIMIT_BYTES,
  uploadObject,
  uploadTemporaryObject,
} from './services/storage';

/** What a temporary upload answered: where the copy landed, or why not. */
export type TemporaryUpload =
  { readonly saved: SavedObject } | { readonly fault: DataFault };

/**
 * One file on its way to a storage.
 *
 * `sent` counts the bytes the browser has sent so far.
 */
export interface Upload {
  readonly storage: string;
  readonly path: string;
  readonly total: number;
  readonly sent: number;
  readonly state: 'sending' | 'done' | 'failed' | 'cancelled';
  readonly fault: DataFault | undefined;
}

const keyOf = (storage: string, path: string): string => `${storage}/${path}`;

/** One entry to move inside one storage. Both paths are relative to its root. */
export interface StorageMove {
  readonly from: string;
  readonly to: string;
}

/** What happened to one entry of a move. The fault is absent when it moved. */
export interface MoveResult {
  readonly from: string;
  readonly fault: DataFault | undefined;
}

/**
 * What one file turned out to be, once a screen asked about it.
 *
 * `details` is what the slicer said, absent for a file that said nothing.
 * `thumbnailUrl` is the address of the picture inside the file, absent for a
 * file that carries none, so a row shows its glyph rather than a broken image.
 */
export interface FileDescription {
  readonly details: PrintDetails | undefined;
  readonly thumbnailUrl: string | undefined;
}

/**
 * A shared store, so a class (svelte-5.md). The client comes in through the
 * constructor, which lets a test pass a fake.
 *
 * The rows are `$state.raw` and are replaced whole on every change. A storage
 * can list thousands of files, so deep proxying each row would cost proxy
 * creation for nothing (svelte-data.md).
 */
export class Storage {
  readonly #client: StorageClient;

  #storages = $state.raw<readonly ServerStorage[]>([]);
  #entries = $state.raw<readonly StorageEntry[]>([]);
  #storage = $state('');
  #directory = $state('');
  #loading = $state(false);
  #loaded = $state(false);
  #error = $state<DataFault | undefined>(undefined);
  #filesLoading = $state(false);
  #filesLoaded = $state(false);
  #filesError = $state<DataFault | undefined>(undefined);
  #uploads = $state.raw<readonly Upload[]>([]);
  #described = $state.raw<Readonly<Record<string, FileDescription>>>({});
  // The read of each file in flight, by key. It is not reactive, because
  // nothing draws it.
  readonly #describing: Record<string, Promise<void> | undefined> = {};
  // The way to stop each upload on its way, by key. It is not reactive,
  // because nothing draws it.
  // eslint-disable-next-line svelte/prefer-svelte-reactivity
  readonly #sending = new Map<string, AbortController>();
  #flight: Promise<void> | undefined;
  #listing: AbortController | undefined;
  #reading: AbortController | undefined;
  #pickerPath = $state('');
  #pickerFolders = $state.raw<readonly StorageEntry[]>([]);
  #pickerError = $state<DataFault | undefined>(undefined);
  #picking: AbortController | undefined;
  // Counts the resets. A request that started before a reset records nothing.
  #resets = 0;

  constructor(client: StorageClient) {
    this.#client = client;
  }

  /** Read-only by design: the collection is the server's (svelte-5.md). */
  get storages(): readonly ServerStorage[] {
    return this.#storages;
  }

  /** What is directly inside the directory being looked at. */
  get entries(): readonly StorageEntry[] {
    return this.#entries;
  }

  /** The storage being listed, empty before one is opened. */
  get storage(): string {
    return this.#storage;
  }

  /** The directory being listed inside that storage. Empty is its root. */
  get directory(): string {
    return this.#directory;
  }

  get loading(): boolean {
    return this.#loading;
  }

  /** True once the storages have answered, which tells an empty server from an unread one. */
  get loaded(): boolean {
    return this.#loaded;
  }

  get error(): DataFault | undefined {
    return this.#error;
  }

  get filesLoading(): boolean {
    return this.#filesLoading;
  }

  get filesLoaded(): boolean {
    return this.#filesLoaded;
  }

  get filesError(): DataFault | undefined {
    return this.#filesError;
  }

  /** The directory a folder picker is showing. Empty is the storage's root. */
  get pickerPath(): string {
    return this.#pickerPath;
  }

  /** The folders directly inside `pickerPath`. Read-only by design: the server owns them. */
  get pickerFolders(): readonly StorageEntry[] {
    return this.#pickerFolders;
  }

  /** Why the picker could not list its directory, as a code. */
  get pickerError(): DataFault | undefined {
    return this.#pickerError;
  }

  /** Every upload this screen started, finished ones included. */
  get uploads(): readonly Upload[] {
    return this.#uploads;
  }

  /** The storages a person may write to, which is where a drop zone sends a file. */
  readonly writable: readonly ServerStorage[] = $derived(
    this.#storages.filter((row) => row.write),
  );

  /** The files in the open directory, folders left out. */
  readonly files: readonly StorageEntry[] = $derived(
    this.#entries.filter((row) => !row.folder),
  );

  /** The folders in the open directory. */
  readonly folders: readonly StorageEntry[] = $derived(
    this.#entries.filter((row) => row.folder),
  );

  /**
   * What one file turned out to be, or undefined until `describe` has answered
   * for it. A screen reads it per row it shows.
   */
  describedFile(storage: string, path: string): FileDescription | undefined {
    return this.#described[keyOf(storage, path)];
  }

  /**
   * Asks the server what is inside one file: the print time, the filament and
   * the rest, and whether it carries a picture.
   *
   * It is per file and not per listing, because the server opens the file to
   * answer. A file already described costs nothing, and a call for a file
   * being described right now waits for that read instead of making another.
   *
   * A failure is recorded as a file that said nothing. A row that cannot be
   * described is still a row a person can select, print and delete.
   *
   * The signal is the caller's, so a screen that has moved to another page
   * stops the reads it left behind. A read that was stopped records nothing:
   * the file was never asked about, and recording it as silent would keep the
   * screen from ever asking again.
   */
  async describe(
    storage: string,
    path: string,
    signal?: AbortSignal,
  ): Promise<void> {
    const key = keyOf(storage, path);

    // A caller joins a read already running. If that read was stopped by the
    // page that started it, it recorded nothing, and this caller reads again.
    for (;;) {
      if (key in this.#described || signal?.aborted === true) return;
      const running = this.#describing[key];
      if (running === undefined) break;
      await running;
    }

    const read = this.#readDescription(key, storage, path, signal);
    this.#describing[key] = read;
    try {
      await read;
    } finally {
      // Only this read's own mark, never the mark of a read after it.
      if (this.#describing[key] === read) this.#describing[key] = undefined;
    }
  }

  // One read of one file. A failure is recorded as a file that said nothing,
  // and a read that was stopped records nothing.
  async #readDescription(
    key: string,
    storage: string,
    path: string,
    signal?: AbortSignal,
  ): Promise<void> {
    const resets = this.#resets;
    try {
      const file = await readStorageEntry(this.#client, storage, path, signal);
      if (resets !== this.#resets) return;
      this.#record(key, {
        details: file.print,
        thumbnailUrl:
          file.print?.thumbnail_width === undefined
            ? undefined
            : thumbnailUrlOf(this.#client, storage, path),
      });
    } catch {
      if (signal?.aborted !== true && resets === this.#resets) {
        this.#record(key, { details: undefined, thumbnailUrl: undefined });
      }
    }
  }

  /**
   * Opens the folder that holds one file and asks what is inside the file.
   * This is what a page about one file needs, with both reads owned here.
   *
   * A second call, or `leaveFile`, stops the file read the first call left.
   * The folder listing is stopped by `open` itself.
   */
  async openFile(storage: string, path: string): Promise<void> {
    this.#reading?.abort();
    const reading = new AbortController();
    this.#reading = reading;

    // The page may join a read the storage page started. If the storage page
    // stops it, `describe` reads again under this page's own signal.
    const folder = path.slice(0, Math.max(path.lastIndexOf('/'), 0));
    await Promise.all([
      this.open(storage, folder),
      this.describe(storage, path, reading.signal),
    ]);
  }

  /** Stops the file read a page left behind when the reader leaves it. */
  leaveFile(): void {
    this.#reading?.abort();
    this.#reading = undefined;
  }

  #record(key: string, described: FileDescription): void {
    this.#described = { ...this.#described, [key]: described };
  }

  /** A second call while one read is in flight is given that read. */
  load(): Promise<void> {
    if (this.#flight) return this.#flight;

    const flight = this.#read().finally(() => {
      if (this.#flight === flight) this.#flight = undefined;
    });
    this.#flight = flight;

    return flight;
  }

  /**
   * Lists one directory. An empty path is the storage's root.
   *
   * A person clicking down a tree leaves a request behind on every click, and
   * a slow one answering after a faster one would put the wrong folder's rows
   * on the screen. The store owns the controller, so opening a folder stops
   * the listing before it, and an answer for a folder that is no longer the
   * open one is dropped (svelte-data.md).
   */
  async open(storage: string, path = ''): Promise<void> {
    this.#listing?.abort();

    const flight = new AbortController();
    this.#listing = flight;
    this.#filesLoading = true;
    this.#storage = storage;
    this.#directory = path;

    try {
      const listing = await readStorageFiles(
        this.#client,
        storage,
        path,
        flight.signal,
      );
      if (this.#stale(flight, storage, path)) return;

      this.#entries = listing.items ?? [];
      this.#filesLoaded = true;
      this.#filesError = undefined;
    } catch (thrown) {
      if (this.#stale(flight, storage, path)) return;

      this.#entries = [];
      this.#filesError = faultOf(thrown);
    } finally {
      if (this.#listing === flight) {
        this.#listing = undefined;
        this.#filesLoading = false;
      }
    }
  }

  // True when this answer is for a directory the person has already left, or
  // for a listing another one replaced. Either way nothing it carries belongs
  // on the screen, the failure an abort raises included.
  #stale(flight: AbortController, storage: string, path: string): boolean {
    return (
      this.#listing !== flight ||
      this.#storage !== storage ||
      this.#directory !== path
    );
  }

  /**
   * Writes one file and lists the directory again. The upload row is added
   * before the request so a screen draws it while the bytes are going up.
   *
   * A file longer than the route accepts is refused here. Sending it would
   * push every byte over the network and be refused at the end, and the
   * person would wait out the whole upload to read that.
   */
  async upload(
    storage: string,
    path: string,
    body: Blob,
  ): Promise<SavedObject | undefined> {
    if (body.size > UPLOAD_LIMIT_BYTES) {
      const fault = this.#tooLarge();
      this.#track({
        storage,
        path,
        total: body.size,
        sent: 0,
        state: 'failed',
        fault,
      });
      this.#error = fault;

      return undefined;
    }

    const key = keyOf(storage, path);
    const flight = new AbortController();
    this.#sending.get(key)?.abort();
    this.#sending.set(key, flight);
    const row = (
      sent: number,
      state: Upload['state'],
      fault: DataFault | undefined,
    ): Upload => ({
      storage,
      path,
      total: body.size,
      sent,
      state,
      fault,
    });
    this.#track(row(0, 'sending', undefined));

    const resets = this.#resets;
    // Whether this flight is still the one a reader would see: no reset
    // happened since it started, and a later upload of the same key has not
    // taken its place. A flight that fails this answers for its caller but
    // writes nothing, so a stale rejection cannot overwrite the row a newer
    // upload owns.
    const current = (): boolean =>
      resets === this.#resets && this.#sending.get(key) === flight;
    try {
      const saved = await uploadObject(
        this.#client,
        storage,
        path,
        body,
        (sent) => {
          if (current()) this.#track(row(sent, 'sending', undefined));
        },
        flight.signal,
      );
      if (!current()) return saved;
      this.#track(row(saved.bytes, 'done', undefined));
      if (storage === this.#storage) await this.open(storage, this.#directory);

      return saved;
    } catch (thrown) {
      if (!current()) return undefined;
      if (flight.signal.aborted) {
        this.#track(row(0, 'cancelled', undefined));

        return undefined;
      }
      const fault = faultOf(thrown);
      this.#track(row(0, 'failed', fault));
      this.#error = fault;

      return undefined;
    } finally {
      if (this.#sending.get(key) === flight) this.#sending.delete(key);
    }
  }

  /**
   * Writes one temporary copy of a file on its way to a printer, and answers
   * where the server put it. The server deletes the copy once it is sent.
   *
   * It is not one of `uploads`, and no listing is read again. The copy is
   * not a file of the storage, so the Storage page does not show it going
   * up either. The caller draws the progress `onProgress` reports. A
   * failure, and an abort through `signal`, answer with the fault. A file
   * longer than the route accepts is refused without a request, for the
   * reason `upload` refuses it.
   */
  async uploadTemporary(
    storage: string,
    name: string,
    body: Blob,
    onProgress?: (sent: number, total: number) => void,
    signal?: AbortSignal,
  ): Promise<TemporaryUpload> {
    if (body.size > UPLOAD_LIMIT_BYTES) return { fault: this.#tooLarge() };

    try {
      return {
        saved: await uploadTemporaryObject(
          this.#client,
          storage,
          name,
          body,
          onProgress,
          signal,
        ),
      };
    } catch (thrown) {
      return { fault: faultOf(thrown) };
    }
  }

  /** Removes one file, then lists the directory again. */
  async remove(storage: string, path: string): Promise<void> {
    const resets = this.#resets;
    try {
      await deleteStorageEntry(this.#client, storage, path);
      if (resets !== this.#resets) return;
      this.#error = undefined;
      if (storage === this.#storage) await this.open(storage, this.#directory);
    } catch (thrown) {
      if (resets === this.#resets) this.#error = faultOf(thrown);
    }
  }

  /** Creates a folder, then lists the directory again. */
  async createFolder(storage: string, path: string): Promise<void> {
    const resets = this.#resets;
    try {
      await createStorageFolder(this.#client, storage, path);
      if (resets !== this.#resets) return;
      this.#error = undefined;
      if (storage === this.#storage) await this.open(storage, this.#directory);
    } catch (thrown) {
      if (resets === this.#resets) this.#error = faultOf(thrown);
    }
  }

  /**
   * Moves entries inside one storage, one at a time, and then lists the
   * directory once. Moves sent at once would race each other's listings.
   *
   * Each entry answers for itself. The first refusal stays in `error`, so a
   * move that went through after it does not hide it. A reset stops the
   * moves not yet sent.
   */
  async moveAll(
    storage: string,
    moves: readonly StorageMove[],
  ): Promise<readonly MoveResult[]> {
    const resets = this.#resets;
    const results: MoveResult[] = [];
    for (const move of moves) {
      // A reset means the person changed, and the moves left were asked for
      // by the person before.
      if (resets !== this.#resets) return results;
      try {
        await moveStorageEntry(this.#client, storage, move.from, move.to);
        results.push({ from: move.from, fault: undefined });
      } catch (thrown) {
        results.push({ from: move.from, fault: faultOf(thrown) });
      }
    }

    if (resets !== this.#resets) return results;
    this.#error = results.find((row) => row.fault !== undefined)?.fault;
    if (storage === this.#storage) await this.open(storage, this.#directory);

    return results;
  }

  /**
   * Lists the folders directly inside one directory, for a picker choosing
   * where something goes. The listing the page shows is left alone.
   *
   * A reader who steps on before the answer arrives is shown the folder they
   * are in. The store stops the read before it and drops a late answer.
   */
  async browseFolders(storage: string, path: string): Promise<void> {
    this.#picking?.abort();
    const flight = new AbortController();
    this.#picking = flight;
    this.#pickerPath = path;

    try {
      const listing = await readStorageFiles(
        this.#client,
        storage,
        path,
        flight.signal,
      );
      if (this.#picking !== flight) return;

      this.#pickerFolders = (listing.items ?? []).filter((row) => row.folder);
      this.#pickerError = undefined;
    } catch (thrown) {
      if (this.#picking !== flight) return;

      this.#pickerFolders = [];
      this.#pickerError = faultOf(thrown);
    }
  }

  /** Where one file's bytes are served from, for a download link. */
  contentUrl(storage: string, path: string): string {
    return contentUrlOf(this.#client, storage, path);
  }

  /** Where the picture inside one file is served from, for an `img` tag. */
  thumbnailUrl(storage: string, path: string): string {
    return thumbnailUrlOf(this.#client, storage, path);
  }

  /** Drops the finished uploads, so a sheet reopened is not showing old rows. */
  clearUploads(): void {
    this.#uploads = this.#uploads.filter((row) => row.state === 'sending');
  }

  /** Stops one upload on its way. Its row says it was cancelled. */
  cancelUpload(storage: string, path: string): void {
    this.#sending.get(keyOf(storage, path))?.abort();
  }

  // The refusal the core would have answered, written here because the
  // request is not made. The code is the core's own, so a screen draws the
  // same sentence it draws for a file the server refused, and `limit_bytes`
  // is the parameter that sentence names.
  #tooLarge(): DataFault {
    return {
      code: BODY_TOO_LARGE,
      traceId: undefined,
      status: undefined,
      params: { limit_bytes: UPLOAD_LIMIT_BYTES },
      message: undefined,
    };
  }

  #track(upload: Upload): void {
    const key = keyOf(upload.storage, upload.path);
    const at = this.#uploads.findIndex(
      (row) => keyOf(row.storage, row.path) === key,
    );
    if (at < 0) {
      this.#uploads = [...this.#uploads, upload];

      return;
    }

    const next = [...this.#uploads];
    next[at] = upload;
    this.#uploads = next;
  }

  /**
   * Forgets every storage, folder, file and upload this store knows, and
   * stops every read on its way. The app calls it when the person signed in
   * changes, so the next person never sees the files of the last one.
   */
  reset(): void {
    this.#resets += 1;
    this.#listing?.abort();
    this.#listing = undefined;
    this.#reading?.abort();
    this.#reading = undefined;
    this.#picking?.abort();
    this.#picking = undefined;
    this.#flight = undefined;
    for (const key of Object.keys(this.#describing)) {
      this.#describing[key] = undefined;
    }
    for (const flight of this.#sending.values()) flight.abort();
    this.#sending.clear();
    this.#storages = [];
    this.#entries = [];
    this.#storage = '';
    this.#directory = '';
    this.#loading = false;
    this.#loaded = false;
    this.#error = undefined;
    this.#filesLoading = false;
    this.#filesLoaded = false;
    this.#filesError = undefined;
    this.#uploads = [];
    this.#described = {};
    this.#pickerPath = '';
    this.#pickerFolders = [];
    this.#pickerError = undefined;
  }

  async #read(): Promise<void> {
    const resets = this.#resets;
    this.#loading = true;
    try {
      const page = await readStorages(this.#client);
      if (resets !== this.#resets) return;
      this.#storages = page.items ?? [];
      this.#loaded = true;
      this.#error = undefined;
    } catch (thrown) {
      if (resets !== this.#resets) return;
      this.#storages = [];
      this.#error = faultOf(thrown);
    } finally {
      if (resets === this.#resets) this.#loading = false;
    }
  }
}

/** One store per sheet, built where the application builds its `PrusaConnect`. */
export const createStorage = (client: StorageClient): Storage =>
  new Storage(client);
