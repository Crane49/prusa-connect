import type { EventType, StreamEvent } from '@prusa/connect';
import { SvelteSet } from 'svelte/reactivity';

import type { Device } from '../domain/command';
import { isPrintFile } from '../domain/print-file';
import type { DeviceStorages } from '../domain/printer-storage';
import { type DataFault, faultOf } from './fault';
import { deviceIdOf, type Fleet, withdrawnOriginOf } from './fleet.svelte';
import { storagesOfEvent } from './printer-storages.svelte';
import {
  type DeviceDetailClient,
  type DeviceEntry,
  type DeviceJobs,
  type DeviceListing,
  DEVICE_FILES_EVENTS,
  deviceThumbnailUrlOf,
  readDevice,
  readFiles,
  readJobs,
  readRecentJobs,
} from './services/device';
import { readDeviceStorages } from './services/print';

/**
 * The types after which a printer's jobs read answers differently. Neither is
 * in the fleet's own subscription, so a host adds them to the one stream it
 * opens.
 */
export const DEVICE_JOB_EVENTS: readonly EventType[] = [
  'com.prusa.connect.device.job.started',
  'com.prusa.connect.device.job.settled',
];

/** What a files event says of one printer folder. */
interface FilesChange {
  /** The storage-prefixed folder, with no leading slash. */
  readonly path: string;
  /** True when its read began, false when the read landed. */
  readonly reading: boolean;
}

/**
 * What a files event says, or undefined for any other event. The payload
 * crosses into this code as `unknown`, so it is narrowed to the fields read
 * here (typescript.md).
 */
const filesChangeOf = (event: StreamEvent): FilesChange | undefined => {
  if (!DEVICE_FILES_EVENTS.includes(event.type)) return undefined;

  const payload = event.payload as
    { path?: unknown; reading?: unknown } | null | undefined;
  if (typeof payload !== 'object' || payload === null) return undefined;
  if (typeof payload.path !== 'string') return undefined;

  return {
    path: payload.path.replace(/^\/+/, ''),
    reading: payload.reading === true,
  };
};

// A gap in the stream may have swallowed a job, a storages or a files event,
// so each is read again after one as well.
const CONTINUITY_LOST = 'com.prusa.connect.stream.continuity.lost';

/** How many ended prints the page reads for its lists of recent prints. */
export const RECENT_PRINTS = 5;

/** What `watch` reads of a fleet. */
export type DeviceDetailFleet = Pick<Fleet, 'subscribe'>;

/** This answers the name a person reads an entry by. */
const nameOf = (entry: DeviceEntry): string => entry.display_name ?? entry.name;

// The folders come first and the rest by name, the order the open folder
// is drawn in.
const byKind = (a: DeviceEntry, b: DeviceEntry): number =>
  Number(b.folder) - Number(a.folder) || nameOf(a).localeCompare(nameOf(b));

/** The fault as it was thrown, and never as a sentence. */
const asError = (thrown: unknown): Error =>
  thrown instanceof Error ? thrown : new Error(String(thrown));

// A folder the server holds nothing of yet. Its empty rows say nothing about
// what the printer has. A held answer carries stale, so a held folder that is
// empty and being read again is not one.
const nothingHeld = (listing: DeviceListing): boolean =>
  listing.reading === true &&
  listing.stale !== true &&
  (listing.items ?? []).length === 0;

/** The storage-prefixed folder that holds a path, with no leading slash. */
const folderOf = (path: string): string => {
  const trimmed = path.replace(/^\/+/, '');

  return trimmed.slice(0, Math.max(0, trimmed.lastIndexOf('/')));
};

// A print names its file as the machine does: with a leading slash, and on a
// FAT drive by the short name. An entry is that file when its path or its
// short name matches, in any case, because FAT ignores case.
const isFileOf = (entry: DeviceEntry, path: string): boolean => {
  const wanted = path.replace(/^\/+/, '').toUpperCase();

  return (
    entry.path.toUpperCase() === wanted ||
    entry.name.toUpperCase() === wanted.slice(wanted.lastIndexOf('/') + 1)
  );
};

/** What one printer's page holds beyond the fleet row. */
export class DeviceDetail {
  readonly #client: DeviceDetailClient;
  readonly #deviceId: string;

  #record = $state.raw<Device | undefined>(undefined);
  #recordLoaded = $state(false);
  #recordError = $state<Error | undefined>(undefined);
  #jobs = $state.raw<DeviceJobs | undefined>(undefined);
  #jobsLoaded = $state(false);
  #jobsError = $state<Error | undefined>(undefined);
  #recent = $state.raw<readonly NonNullable<DeviceJobs['last']>[] | undefined>(
    undefined,
  );

  #entries = $state.raw<readonly DeviceEntry[]>([]);
  #directory = $state('');
  #filesLoading = $state(false);
  #filesRefreshing = $state(false);
  // The folders expanded under their own rows in the open folder, each with
  // its rows once the printer has listed them.
  #expanded = $state.raw<
    Readonly<Record<string, readonly DeviceEntry[] | undefined>>
  >({});
  #filesLoaded = $state(false);
  #filesError = $state<Error | DataFault | undefined>(undefined);
  #filesReadingSince = $state<string | undefined>(undefined);
  // The server's own reading_since of that read, which only tells one read
  // from the next: its clock need not agree with the browser's.
  #readBegan: string | undefined;
  #filesUnread = $state(false);
  // The folders of this printer being read now.
  readonly #reading = new SvelteSet<string>();
  // Whether the printer holds the file each print names, by the path the
  // print names it by. A path is absent until its folder has listed.
  #held = $state.raw<Readonly<Record<string, boolean>>>({});
  // The paths whose folder is being listed, or read by the printer. Nothing
  // draws it.
  // eslint-disable-next-line svelte/prefer-svelte-reactivity
  readonly #checking = new Set<string>();
  // The checked paths of a print that was stopped or failed, until the
  // printer itself has been asked to read their folder. Nothing draws it.
  // eslint-disable-next-line svelte/prefer-svelte-reactivity
  readonly #interrupted = new Set<string>();

  #storages = $state.raw<DeviceStorages | undefined>(undefined);
  #storagesChecking = $state(false);
  #storagesError = $state<Error | undefined>(undefined);
  #storagesFlight: AbortController | undefined;

  #flight: Promise<void> | undefined;
  #listing: AbortController | undefined;
  // A folder has been opened, so the Files tab is on this page.
  #browsing = false;
  // The read in flight for each expanded folder. A newer read of a folder
  // aborts the older one, so an old answer never replaces a newer one.
  #relisting: Record<string, AbortController | undefined> = {};

  constructor(client: DeviceDetailClient, deviceId: string) {
    this.#client = client;
    this.#deviceId = deviceId;
  }

  /** The printer this store is about. Read-only by design. */
  get deviceId(): string {
    return this.#deviceId;
  }

  /** The printer's own record, once `loadRecord` has answered. */
  get record(): Device | undefined {
    return this.#record;
  }

  /** True once `loadRecord` has answered, whether or not the printer exists. */
  get recordLoaded(): boolean {
    return this.#recordLoaded;
  }

  /** What the record read failed with, and undefined after a read that answered. */
  get recordError(): Error | undefined {
    return this.#recordError;
  }

  /**
   * Reads the printer's record on its own. The fleet holds only printers that
   * are here now, so a retired printer's page takes its record from here.
   */
  async loadRecord(): Promise<void> {
    try {
      this.#record = await readDevice(this.#client, this.#deviceId);
      this.#recordError = undefined;
    } catch (thrown) {
      this.#record = undefined;
      this.#recordError = asError(thrown);
    } finally {
      this.#recordLoaded = true;
    }
  }

  /** The print on the bed and the one before it, once the core has answered. */
  get jobs(): DeviceJobs | undefined {
    return this.#jobs;
  }

  /** True once the jobs read has answered, which tells an empty answer from an unread one. */
  get jobsLoaded(): boolean {
    return this.#jobsLoaded;
  }

  get jobsError(): Error | undefined {
    return this.#jobsError;
  }

  /**
   * The newest prints that have ended, newest first. Undefined until the read
   * has answered and after a refusal, where a screen falls back to the one
   * print `jobs.last` names. Read-only by design.
   */
  get recent(): readonly NonNullable<DeviceJobs['last']>[] | undefined {
    return this.#recent;
  }

  /** What is directly inside the directory being looked at. */
  get entries(): readonly DeviceEntry[] {
    return this.#entries;
  }

  /** The storage-prefixed directory being listed. Empty is the storages themselves. */
  get directory(): string {
    return this.#directory;
  }

  get filesLoading(): boolean {
    return this.#filesLoading;
  }

  /** The open folder is being read again, and its rows stay on screen. */
  get filesRefreshing(): boolean {
    return this.#filesRefreshing;
  }

  /**
   * The folders expanded under their own rows, keyed by path. A folder whose
   * listing has not answered yet maps to undefined. It is read-only by design.
   */
  get expanded(): Readonly<Record<string, readonly DeviceEntry[] | undefined>> {
    return this.#expanded;
  }

  get filesLoaded(): boolean {
    return this.#filesLoaded;
  }

  /** Why the open folder would not list: a thrown error, or the fault the printer's read left. */
  get filesError(): Error | DataFault | undefined {
    return this.#filesError;
  }

  /**
   * When this page heard that the printer is reading the open folder, by the
   * browser's clock, while it reads it.
   */
  get filesReadingSince(): string | undefined {
    return this.#filesReadingSince;
  }

  /** The printer is reading the open folder and nothing of it is held yet. */
  get filesUnread(): boolean {
    return this.#filesUnread;
  }

  /** Some folder of this printer is being read, which holds up its other reads. */
  get readingDrive(): boolean {
    return this.#reading.size > 0;
  }

  /** What the printer said its storages are, once a read or an event has answered. */
  get storages(): DeviceStorages | undefined {
    return this.#storages;
  }

  /** The printer is being asked again for its storages. */
  get storagesChecking(): boolean {
    return this.#storagesChecking;
  }

  get storagesError(): Error | undefined {
    return this.#storagesError;
  }

  /**
   * Reads the printer's storages. With `refresh` the server asks the printer
   * again, which is how a USB drive put in since is found. A newer read aborts
   * an older one, so an old answer never replaces a newer one.
   */
  async readStorages(options: { refresh?: boolean } = {}): Promise<void> {
    this.#storagesFlight?.abort();
    const flight = new AbortController();
    this.#storagesFlight = flight;
    this.#storagesChecking = true;

    try {
      const answer = await readDeviceStorages(this.#client, this.#deviceId, {
        refresh: options.refresh === true,
        signal: flight.signal,
      });
      if (this.#storagesFlight !== flight) return;

      this.#storages = answer;
      this.#storagesError = undefined;
    } catch (thrown) {
      if (this.#storagesFlight !== flight) return;

      this.#storagesError = asError(thrown);
    } finally {
      if (this.#storagesFlight === flight) {
        this.#storagesFlight = undefined;
        this.#storagesChecking = false;
      }
    }
  }

  /**
   * Where the picture inside one print file on the printer is served from, or
   * undefined for a file that is not a print file. A file the open listing
   * holds is named by the version it was listed at, so the address changes
   * when the file does.
   */
  thumbnailUrl(path: string): string | undefined {
    if (!isPrintFile(path)) return undefined;

    const listed =
      this.#entries.find((entry) => entry.path === path) ??
      Object.values(this.#expanded)
        .flatMap((rows) => rows ?? [])
        .find((entry) => entry.path === path);

    return deviceThumbnailUrlOf(this.#client, this.#deviceId, path, listed);
  }

  /**
   * Whether the printer still holds the file a print names. It is undefined
   * until `checkHeld` has listed the file's folder, and for a folder that
   * would not list.
   */
  holds(path: string): boolean | undefined {
    return this.#held[path];
  }

  /**
   * Lists the folder of each path not checked yet, and records whether the
   * printer holds the file. A file held from before by a print in
   * `interrupted` has the printer read its folder again: firmware 8.0.0
   * removes the partial file of a stopped send on its own (CL-275).
   */
  async checkHeld(
    paths: readonly string[],
    interrupted: readonly string[] = [],
  ): Promise<void> {
    // The check marks its path before its first read, so a path named twice
    // is listed once, and each folder is read once for all its paths.
    const fresh = paths.filter(
      (path) => !Object.hasOwn(this.#held, path) && !this.#checking.has(path),
    );
    for (const path of fresh) {
      this.#checking.add(path);
      if (interrupted.includes(path)) this.#interrupted.add(path);
    }
    await Promise.all(
      fresh
        .map(folderOf)
        .filter((folder, at, all) => all.indexOf(folder) === at)
        .map((folder) => this.#check(folder, false)),
    );
  }

  // Decides every path checked in `folder` from one listing of it. A path
  // whose folder is still being read waits for its files event.
  async #check(folder: string, refresh: boolean): Promise<void> {
    let listing: DeviceListing | undefined;
    try {
      listing = await readFiles(
        this.#client,
        this.#deviceId,
        folder,
        undefined,
        refresh,
      );
    } catch {
      // The file stays unknown and Print again stays offered. The printer
      // still refuses a file it does not hold.
    }
    const waiting = [...this.#checking].some(
      (path) => folderOf(path) === folder,
    );
    // An answer that comes after the read landed decides nothing and marks
    // nothing.
    if (!waiting) return;
    this.#mark(folder, listing?.reading === true);
    if (listing?.reading === true) return;

    let ask = false;
    for (const path of this.#checking) {
      if (folderOf(path) !== folder) continue;
      const held =
        listing !== undefined &&
        listing.fault === undefined &&
        (listing.items ?? []).some((entry) => isFileOf(entry, path));
      if (held && this.#interrupted.delete(path)) {
        ask = true;
        continue;
      }
      this.#interrupted.delete(path);
      this.#checking.delete(path);
      if (listing === undefined || listing.fault !== undefined) continue;

      this.#held = { ...this.#held, [path]: held };
    }
    if (ask) await this.#check(folder, true);
  }

  /** Reads what this page needs beyond the fleet row. */
  load(): Promise<void> {
    if (this.#flight) return this.#flight;

    // The printer reports a drive put in or pulled out on its own, and the
    // server publishes it as storages.changed, so what it holds is current.
    const flight = Promise.all([
      this.#readJobs(),
      this.openFolder(''),
      this.readStorages(),
    ])
      .then(() => undefined)
      .finally(() => {
        if (this.#flight === flight) this.#flight = undefined;
      });
    this.#flight = flight;

    return flight;
  }

  /**
   * Reads the jobs again whenever a print of this printer starts or ends, and
   * takes the storages whenever a USB drive is put in or pulled out, off the
   * events the fleet applies. The returned function stops listening.
   */
  watch(fleet: DeviceDetailFleet): () => void {
    return fleet.subscribe((event) => {
      const gone = withdrawnOriginOf(event);
      if (gone?.kind === 'device' && gone.id === this.#deviceId) {
        this.#record = undefined;
        this.#recordLoaded = true;
      }

      const ours = deviceIdOf(event) === this.#deviceId;
      if (ours && DEVICE_JOB_EVENTS.includes(event.type)) void this.#readJobs();
      // A gap may have swallowed any kind, so each is read again.
      if (event.type === CONTINUITY_LOST) {
        void this.#readJobs();
        void this.readStorages();
        this.#rereadAll();
      }

      const files = ours ? filesChangeOf(event) : undefined;
      if (files?.reading === true) this.#reading.add(files.path);
      if (files?.reading === false) {
        this.#reading.delete(files.path);
        void this.#landed(files.path);
      }

      const storages = ours ? storagesOfEvent(event) : undefined;
      if (storages !== undefined) this.#storagesChanged(storages);
    });
  }

  // A drive that went away takes the open folder with it, and a drive that
  // came has to be listed before it can be opened, so either way the listing
  // starts again from the storages.
  #storagesChanged(storages: DeviceStorages): void {
    this.#storagesFlight?.abort();
    this.#storagesFlight = undefined;
    this.#storagesChecking = false;
    this.#storages = storages;
    this.#storagesError = undefined;

    const drive = this.#directory.split('/')[0] ?? '';
    const held = (storages.items ?? []).some(
      (storage) => storage.name === drive,
    );
    if (drive === '' || !held) void this.openFolder('');
  }

  /**
   * Lists one directory. An empty path is the storages themselves.
   *
   * The server answers at once with what it holds, which the printer's own
   * file events keep current, and reads a folder it holds nothing of. With
   * `refresh` the printer reads the folder again, for a change made on its
   * screen, which it reports to nobody. A read landing is a files event,
   * which reads the folder again. Reading the folder already on screen keeps
   * its rows there until the answer comes.
   */
  openFolder(path: string, refresh = false): Promise<void> {
    this.#browsing = true;

    return this.#list(path, refresh);
  }

  // A newer listing aborts the older one, so the landed event that overtakes
  // the answer to the ask that started its read is answered by a newer ask.
  async #list(path: string, refresh: boolean): Promise<void> {
    this.#listing?.abort();

    const flight = new AbortController();
    this.#listing = flight;
    const again = path === this.#directory && this.#filesLoaded;
    if (again) {
      this.#filesRefreshing = true;
    } else {
      // The rows of the folder left behind must not read as this folder's:
      // the Files panel takes the root's rows as the drives and opens the first.
      this.#filesLoading = true;
      this.#filesLoaded = false;
      this.#entries = [];
      this.#filesError = undefined;
      this.#filesReadingSince = undefined;
      this.#filesUnread = false;
      this.#expanded = {};
    }
    this.#directory = path;

    try {
      const listing = await readFiles(
        this.#client,
        this.#deviceId,
        path,
        flight.signal,
        refresh,
      );
      if (this.#stale(flight, path)) return;

      this.#entries = listing.items ?? [];
      this.#filesLoaded = true;
      this.#filesError =
        listing.fault === undefined ? undefined : faultOf(listing.fault);
      if (listing.reading !== true) {
        this.#filesReadingSince = undefined;
      } else if (
        this.#filesReadingSince === undefined ||
        listing.reading_since !== this.#readBegan
      ) {
        this.#readBegan = listing.reading_since;
        // Only its string is kept, so nothing reads the Date as it changes.
        // eslint-disable-next-line svelte/prefer-svelte-reactivity
        this.#filesReadingSince = new Date().toISOString();
      }
      this.#filesUnread = nothingHeld(listing);
      this.#mark(path, listing.reading === true);
      if (!this.#filesUnread) this.#prune(path, this.#entries);
    } catch (thrown) {
      if (this.#stale(flight, path)) return;

      this.#entries = [];
      this.#filesReadingSince = undefined;
      this.#filesUnread = false;
      this.#reading.delete(path);
      this.#filesError = asError(thrown);
    } finally {
      if (this.#listing === flight) {
        this.#listing = undefined;
        this.#filesLoading = false;
        this.#filesRefreshing = false;
      }
    }
  }

  /** Lists a folder under its own row in the open folder. */
  async expand(path: string): Promise<void> {
    this.#expanded = { ...this.#expanded, [path]: undefined };
    await this.#relist(path, false);
  }

  /** Folds an expanded folder away, and drops a read of it in flight. */
  collapse(path: string): void {
    this.#relisting[path]?.abort();
    this.#expanded = Object.fromEntries(
      Object.entries(this.#expanded).filter(([held]) => held !== path),
    );
  }

  /**
   * Reads again the folder that holds this path, where that folder is on
   * screen, either open or expanded. A write or a landed transfer calls it.
   */
  changedAt(path: string): Promise<void> {
    const folder = folderOf(path);
    // What the printer holds in the folder is decided again from what the
    // server holds, which the write already changed. Forgetting it instead
    // has the printer read the whole folder again after every send.
    for (const held of Object.keys(this.#held)) {
      if (folderOf(held) === folder) this.#checking.add(held);
    }

    return this.#landed(folder);
  }

  // Reads what the server now holds of a folder, with no new read of the
  // printer, wherever the page shows it or waits on it. A folder still
  // expanding is read again too: the newer read replaces the first, so a
  // change made meanwhile is not missed.
  async #landed(folder: string): Promise<void> {
    const reads: Promise<void>[] = [];
    if (this.#browsing && folder === this.#directory) {
      reads.push(this.#list(folder, false));
    }
    if (Object.hasOwn(this.#expanded, folder)) {
      reads.push(this.#relist(folder, false));
    }
    if ([...this.#checking].some((path) => folderOf(path) === folder)) {
      reads.push(this.#check(folder, false));
    }
    await Promise.all(reads);
  }

  // A gap may have swallowed the files event of any folder the page shows or
  // waits on, so each is read again with refresh off. An answer that still
  // says reading marks its folder again.
  #rereadAll(): void {
    this.#reading.clear();
    const folders = [
      this.#directory,
      ...Object.keys(this.#expanded),
      ...[...this.#checking].map(folderOf),
    ].filter((folder, at, all) => all.indexOf(folder) === at);
    for (const folder of folders) void this.#landed(folder);
  }

  #mark(folder: string, reading: boolean): void {
    if (reading) this.#reading.add(folder);
    else this.#reading.delete(folder);
  }

  // Lists an expanded folder. A folder folded away meanwhile stays folded,
  // one that will not list folds back, and one the server holds nothing of
  // yet stays unanswered until its files event.
  async #relist(path: string, refresh: boolean): Promise<void> {
    this.#relisting[path]?.abort();
    const flight = new AbortController();
    this.#relisting[path] = flight;

    try {
      const listing = await readFiles(
        this.#client,
        this.#deviceId,
        path,
        flight.signal,
        refresh,
      );
      if (this.#relisting[path] !== flight) return;
      if (!Object.hasOwn(this.#expanded, path)) return;
      if (listing.fault !== undefined) {
        this.collapse(path);
        return;
      }
      this.#mark(path, listing.reading === true);
      if (nothingHeld(listing)) return;

      const listed = [...(listing.items ?? [])].sort(byKind);
      this.#expanded = { ...this.#expanded, [path]: listed };
      this.#prune(path, listed);
    } catch {
      if (this.#relisting[path] !== flight) return;
      this.collapse(path);
    } finally {
      if (this.#relisting[path] === flight) this.#relisting[path] = undefined;
    }
  }

  // Drops every expanded folder under `folder` whose branch the new listing
  // of `folder` no longer holds, because the printer no longer has it.
  #prune(folder: string, entries: readonly DeviceEntry[]): void {
    const kept = entries.map((entry) => entry.path);
    const prefix = folder === '' ? '' : `${folder}/`;
    const gone = Object.keys(this.#expanded).filter((path) => {
      if (!path.startsWith(prefix) || path === folder) return false;
      const child = prefix + (path.slice(prefix.length).split('/')[0] ?? '');

      return !kept.includes(child);
    });
    for (const path of gone) this.collapse(path);
  }

  // True when this answer is for a folder the person has already left.
  #stale(flight: AbortController, path: string): boolean {
    return this.#listing !== flight || this.#directory !== path;
  }

  // The print on the bed and the recent ones are two reads side by side. The
  // jobs read is what says whether this printer's prints can be read at all,
  // so a refused history leaves the older prints out and nothing else.
  async #readJobs(): Promise<void> {
    await Promise.all([this.#readCurrent(), this.#readRecent()]);
  }

  async #readCurrent(): Promise<void> {
    try {
      this.#jobs = await readJobs(this.#client, this.#deviceId);
      this.#jobsLoaded = true;
      this.#jobsError = undefined;
    } catch (thrown) {
      this.#jobs = undefined;
      this.#jobsError = asError(thrown);
    }
  }

  async #readRecent(): Promise<void> {
    try {
      const page = await readRecentJobs(
        this.#client,
        this.#deviceId,
        RECENT_PRINTS,
      );
      this.#recent = page.items ?? [];
    } catch {
      this.#recent = undefined;
    }
  }
}
