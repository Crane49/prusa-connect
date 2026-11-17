// A version being put together: the files a person dropped, each on its way
// up to the storage, and the files of the newest version they keep.
//
// The files go up as soon as they are dropped, so the person can write the
// note while the bytes travel. Each one goes to `<dir>/<upload>/<name>`, where
// dir is the part's own folder the module named and upload is one segment for
// this draft, so two drafts never write over each other. A name sent a second
// time goes under a segment of its own, so two uploads in flight never write
// the same path and the row marked uploaded names exactly the bytes it sent.
//
// Nothing this draft uploaded outlives it unless it was saved. A file taken
// out is deleted, and one taken out while still on its way is deleted once it
// lands. A draft thrown away does the same for every file. A draft is thrown
// away whenever its form goes, also when the person leaves the part without
// closing the dialog, and a draft being saved waits for the answer: saved, it
// keeps its files; refused, it deletes them.
//
// A class, because the dialog and its rows share it (svelte-5.md). One draft
// is made each time the dialog opens and dropped when it closes.
import type { FilesService } from './files';
import { byteLength, clipBytes, MAX_FILE_NAME_BYTES } from './presentation';
import type { FileDraft, PartDetail, PartFile } from './parts';

/** Where the draft itself is: being put together, being saved, saved, or thrown away. */
type Phase = 'open' | 'saving' | 'saved' | 'discarded';

/** Where one dropped file is. */
export type UploadState = 'sending' | 'done' | 'failed';

/** One file the person dropped. */
export interface Upload {
  /** Stable for the life of the draft, for the row's key. */
  readonly key: string;
  readonly name: string;
  readonly size: number;
  readonly path: string;
  readonly state: UploadState;
  /** What the storage threw, for a file that failed. */
  readonly fault?: unknown;
}

/** One file of the newest version, and whether the new version keeps it. */
export interface Kept {
  readonly file: PartFile;
  readonly kept: boolean;
}

/** The highest code point that is a control character, and the one above the printable range. */
const LAST_CONTROL = 0x1f;
const DELETE = 0x7f;
/** The C1 control characters, which the module refuses in a file name too. */
const FIRST_C1 = 0x80;
const LAST_C1 = 0x9f;

/** Whether one character cannot be carried into a path segment. */
const unsafe = (character: string): boolean => {
  const code = character.codePointAt(0) ?? 0;

  return (
    character === '/' ||
    character === '\\' ||
    code <= LAST_CONTROL ||
    code === DELETE ||
    (code >= FIRST_C1 && code <= LAST_C1)
  );
};

/** The longest extension kept whole when a long name is clipped. */
const KEPT_EXTENSION_BYTES = 16;

/**
 * A long name clipped to what the module records, keeping its extension, so
 * a print file still reads as one.
 */
const fitted = (name: string): string => {
  if (byteLength(name) <= MAX_FILE_NAME_BYTES) return name;

  const dot = name.lastIndexOf('.');
  const extension = dot > 0 ? name.slice(dot) : '';
  if (extension === '' || byteLength(extension) > KEPT_EXTENSION_BYTES) {
    return clipBytes(name, MAX_FILE_NAME_BYTES);
  }

  const stem = clipBytes(
    name.slice(0, dot),
    MAX_FILE_NAME_BYTES - byteLength(extension),
  );

  return stem.trimEnd() + extension;
};

/** A file name as one path segment the module accepts. */
export const segmentOf = (name: string): string => {
  const written = fitted(
    Array.from(name, (character) => (unsafe(character) ? '-' : character))
      .join('')
      .trim(),
  );

  return written === '' || written === '.' || written === '..'
    ? 'file'
    : written;
};

/** One upload segment: lower-case letters and digits, as the module reads one. */
export const uploadSegment = (): string => {
  const bytes = new Uint8Array(6);
  crypto.getRandomValues(bytes);

  return Array.from(bytes, (byte) => byte.toString(36).padStart(2, '0'))
    .join('')
    .slice(0, 10);
};

export class VersionDraft {
  readonly #files: FilesService;
  readonly #storage: string;
  readonly #dir: string;
  readonly #upload = uploadSegment();
  #counter = 0;
  // The two sets below are bookkeeping nothing renders, so they need no
  // reactivity, and `svelte/reactivity` is not in the host's import map.
  /** Every path this draft sent a file to, so no path is written twice. */
  // eslint-disable-next-line svelte/prefer-svelte-reactivity
  readonly #paths = new Set<string>();
  /** The rows taken out while their file was on its way, deleted once it lands. */
  // eslint-disable-next-line svelte/prefer-svelte-reactivity
  readonly #unwanted = new Set<string>();
  /** Set when the draft was thrown away while it was being saved. */
  #discardIfRefused = false;

  #phase = $state<Phase>('open');
  #uploads = $state.raw<readonly Upload[]>([]);
  #kept = $state.raw<readonly Kept[]>([]);
  #note = $state('');

  constructor(files: FilesService, part: PartDetail) {
    this.#files = files;
    this.#storage = part.storage;
    this.#dir = part.dir;

    // The new version starts as the newest one: every file kept, so a person
    // who adds one drawing does not have to pick the model again.
    const latest = part.versions.at(-1);
    this.#kept = (latest?.files ?? []).map((file) => ({ file, kept: true }));
  }

  /** What the person dropped, in the order they dropped it. Read-only by design. */
  get uploads(): readonly Upload[] {
    return this.#uploads;
  }

  /** The files of the newest version. Read-only by design. */
  get kept(): readonly Kept[] {
    return this.#kept;
  }

  get note(): string {
    return this.#note;
  }

  set note(next: string) {
    this.#note = next;
  }

  /** True while a file is still on its way up. */
  get sending(): boolean {
    return this.#uploads.some((row) => row.state === 'sending');
  }

  /** True while the version is being recorded. Nothing may change then. */
  get saving(): boolean {
    return this.#phase === 'saving';
  }

  /** The files the version will hold, kept and uploaded, in that order. */
  get files(): FileDraft[] {
    return [
      ...this.#kept
        .filter((row) => row.kept)
        .map((row) => ({ storage: row.file.storage, path: row.file.path })),
      ...this.#uploads
        .filter((row) => row.state === 'done')
        .map((row) => ({ path: row.path })),
    ];
  }

  /** True when the version has a file and nothing is still on its way. */
  get ready(): boolean {
    return this.#phase === 'open' && !this.sending && this.files.length > 0;
  }

  /** Keeps a file of the newest version, or leaves it out. */
  keep(file: PartFile, kept: boolean): void {
    if (this.#phase !== 'open') return;
    this.#kept = this.#kept.map((row) =>
      row.file.path === file.path && row.file.storage === file.storage
        ? { file: row.file, kept }
        : row,
    );
  }

  /**
   * Sends each file to the storage. A second file of a name this draft
   * already holds replaces the first, and the first is deleted, now or once
   * it lands.
   */
  add(files: readonly File[]): void {
    if (this.#phase !== 'open') return;

    for (const file of files) {
      const name = segmentOf(file.name);
      this.#counter += 1;
      const key = String(this.#counter);
      const path = this.#pathFor(name, key);

      const replaced = this.#uploads.filter((row) => row.name === name);
      this.#uploads = [
        ...this.#uploads.filter((row) => row.name !== name),
        { key, name, size: file.size, path, state: 'sending' },
      ];
      for (const row of replaced) void this.#letGo(row);

      void this.#send(key, path, file);
    }
  }

  /** Takes one dropped file back out, and deletes it from the storage. */
  async drop(key: string): Promise<void> {
    if (this.#phase !== 'open') return;
    const row = this.#uploads.find((upload) => upload.key === key);
    if (row === undefined) return;
    this.#uploads = this.#uploads.filter((upload) => upload.key !== key);
    await this.#letGo(row);
  }

  /**
   * Records the version through `record`, which answers whether it landed.
   * Nothing can be added or taken out meanwhile, and a draft thrown away
   * meanwhile keeps its files until the answer says whether they are the
   * version's now.
   */
  async save(
    record: (note: string, files: FileDraft[]) => Promise<boolean>,
  ): Promise<boolean> {
    if (!this.ready) return false;
    this.#phase = 'saving';

    let saved = false;
    try {
      saved = await record(this.#note.trim(), this.files);
    } finally {
      this.#phase = saved ? 'saved' : 'open';
    }

    if (!saved && this.#discardIfRefused) await this.discard();

    return saved;
  }

  /**
   * Deletes everything this draft uploaded, when it goes without being
   * saved. A file still on its way is deleted once it lands. A saved draft
   * deletes nothing: its files are the version's. A delete that fails leaves
   * the file in the part's folder, which deleting the part removes.
   */
  async discard(): Promise<void> {
    if (this.#phase === 'saved' || this.#phase === 'discarded') return;
    if (this.#phase === 'saving') {
      this.#discardIfRefused = true;

      return;
    }

    this.#phase = 'discarded';
    const rows = this.#uploads;
    this.#uploads = [];
    await Promise.allSettled(rows.map((row) => this.#letGo(row)));
  }

  /**
   * The path one file goes to: the draft's own segment the first time a
   * name is sent, and a segment of its own after, so an upload still on its
   * way can never land over the bytes of a later one.
   */
  #pathFor(name: string, key: string): string {
    const first = `${this.#dir}/${this.#upload}/${name}`;
    const path = this.#paths.has(first)
      ? `${this.#dir}/${this.#upload}-${key}/${name}`
      : first;
    this.#paths.add(path);

    return path;
  }

  /** Deletes the file of a row taken out: now when it landed, once it lands when it is on its way. */
  async #letGo(row: Upload): Promise<void> {
    if (row.state === 'sending') this.#unwanted.add(row.key);
    else if (row.state === 'done') await this.#forget(row.path);
  }

  async #send(key: string, path: string, body: Blob): Promise<void> {
    let landed = false;
    let fault: unknown;
    try {
      await this.#files.upload(this.#storage, path, body);
      landed = true;
    } catch (thrown) {
      fault = thrown;
    }

    if (this.#unwanted.delete(key)) {
      if (landed) await this.#forget(path);

      return;
    }

    if (landed) this.#mark(key, 'done');
    else this.#mark(key, 'failed', fault);
  }

  #mark(key: string, state: UploadState, fault?: unknown): void {
    this.#uploads = this.#uploads.map((row) =>
      row.key === key ? { ...row, state, fault } : row,
    );
  }

  async #forget(path: string): Promise<void> {
    try {
      await this.#files.remove(this.#storage, path);
    } catch {
      // Left in the part's folder, which deleting the part removes.
    }
  }
}
