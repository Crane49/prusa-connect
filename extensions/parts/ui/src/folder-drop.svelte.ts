// Files dropped onto a folder, each becoming a part of that folder.
//
// A part is a name and versions of files, so a file dropped onto a folder
// becomes a new part named after the file, with one version that holds it.
// Files that differ only by their extension are one part: `hook.stl` and
// `hook.bgcode` dropped together are the model and the print of one hook.
//
// Each part is made, then its files go up into the part's own folder in the
// storage, then the module records them as the first version: the same three
// calls the new-part dialog and the version dialog make one after another. A
// file the storage refuses takes its part back out, so a failed drop leaves
// no empty part behind. When taking it back out fails too, the row says what
// is left: the part itself, which links to its page to be deleted there, or
// files in the storage. Leaving the page in the middle of a drop ends both
// the upload and the cleanup, and the row says that as well.
//
// The parts are made one at a time, in the order they were dropped, so the
// ids follow that order. The writes go through the catalogue's quiet lane:
// a drop keeps its refusals on its own rows and never touches the busy flag
// or the fault a dialog is waiting on.
//
// A class, because the tree, the listing and the list of uploads share it
// (svelte-5.md). The catalogue and the files service come in through the
// constructor, so a test drives it without a network.
import type { Catalogue } from './catalogue.svelte';
import { extensionOf, groupByStem } from './drop-groups';
import type { FilesService } from './files';
import type { PartDetail } from './parts';
import { segmentOf, uploadSegment } from './version-draft.svelte';

/** Where one dropped part is. */
export type DropState = 'waiting' | 'sending' | 'done' | 'failed' | 'refused';

/**
 * What a failed drop could not take back out: the part, which is still in
 * the catalogue, or files the storage kept after the part went.
 */
export type DropLeftover = 'part' | 'files';

/** One part being made from dropped files. */
export interface DropJob {
  /** Stable for the life of the page, for the row's key. */
  readonly key: string;
  /** The folder the files were dropped onto, 0 for the top. */
  readonly folder: number;
  /** The name the part is given: the file name without its extension. */
  readonly name: string;
  /** The names of the files, as the person dropped them. */
  readonly files: readonly string[];
  readonly state: DropState;
  /** The part, once it was made and its files recorded, or when a failed one is left behind. */
  readonly part?: number;
  /** What the core threw, for a job that failed. */
  readonly fault?: unknown;
  /** What a failed job left behind, when taking its part back out failed too. */
  readonly left?: DropLeftover;
  /** The folder in the storage the files were left in. */
  readonly leftIn?: string;
  /** What taking the part back out threw. */
  readonly leftFault?: unknown;
}

export class FolderDrop {
  readonly #catalogue: Catalogue;
  readonly #files: FilesService;
  #counter = 0;
  /** The queue of parts still to make, run one at a time. */
  #running: Promise<void> = Promise.resolve();

  #jobs = $state.raw<readonly DropJob[]>([]);

  constructor(catalogue: Catalogue, files: FilesService) {
    this.#catalogue = catalogue;
    this.#files = files;
  }

  /** Every part made from a drop since the page opened. Read-only by design. */
  get jobs(): readonly DropJob[] {
    return this.#jobs;
  }

  /** True while a part is still being made or waits for its turn. */
  get sending(): boolean {
    return this.#jobs.some(
      (job) => job.state === 'waiting' || job.state === 'sending',
    );
  }

  /** True when the person may drop files: they may change the catalogue and write files. */
  get allowed(): boolean {
    return this.#catalogue.allowed.write && this.#catalogue.allowed.upload;
  }

  /**
   * Makes one part of each group of files dropped onto a folder. Answers when
   * every part of this drop was made or refused.
   */
  drop(folder: number, files: readonly File[]): Promise<void> {
    if (!this.allowed || !Number.isInteger(folder) || folder < 0) {
      return Promise.resolve();
    }

    const accepted = this.#catalogue.storage?.allowed_extensions ?? [];
    const groups = groupByStem(files);
    if (groups.length === 0) return Promise.resolve();

    // An empty list is the server saying the storage takes any extension.
    const takes = (file: File): boolean =>
      accepted.length === 0 || accepted.includes(extensionOf(file.name));

    // A group splits by what the storage takes: the files it takes become
    // the part, and the ones it does not are a row of their own, so one
    // refused drawing does not take the model it was dropped with down too.
    const made = groups.flatMap((group) => {
      const kept = group.files.filter(takes);
      const left = group.files.filter((file) => !takes(file));
      const rows: { job: DropJob; files: readonly File[] }[] = [];

      for (const [files, state] of [
        [kept, 'waiting'],
        [left, 'refused'],
      ] as const) {
        if (files.length === 0) continue;
        this.#counter += 1;
        rows.push({
          job: {
            key: String(this.#counter),
            folder,
            name: group.name,
            files: files.map((file) => file.name),
            state,
          },
          files,
        });
      }

      return rows;
    });

    this.#jobs = [...this.#jobs, ...made.map((row) => row.job)];

    const waiting = made.filter((row) => row.job.state === 'waiting');
    const run = this.#running.then(async () => {
      for (const row of waiting) await this.#make(row.job, row.files);
      if (waiting.length > 0) await this.#catalogue.relist();
    });
    this.#running = run;

    return run;
  }

  /** Takes every finished row off the list. */
  clear(): void {
    const left = this.#jobs.filter(
      (job) => job.state === 'waiting' || job.state === 'sending',
    );
    if (left.length !== this.#jobs.length) this.#jobs = left;
  }

  async #make(job: DropJob, files: readonly File[]): Promise<void> {
    this.#mark(job.key, { state: 'sending' });

    let part: PartDetail;
    try {
      part = await this.#catalogue.addPartQuietly({
        name: job.name,
        folder: job.folder,
      });
    } catch (thrown) {
      this.#mark(job.key, { state: 'failed', fault: thrown });

      return;
    }

    const upload = uploadSegment();

    try {
      const paths: string[] = [];
      for (const file of files) {
        const path = `${part.dir}/${upload}/${segmentOf(file.name)}`;
        await this.#files.upload(part.storage, path, file);
        paths.push(path);
      }

      await this.#catalogue.addVersionQuietly(
        part.id,
        '',
        paths.map((path) => ({ path })),
      );

      this.#mark(job.key, { state: 'done', part: part.id });
    } catch (thrown) {
      // The part holds nothing the person dropped, so it goes too, and its
      // folder in the storage with whatever reached it.
      this.#mark(job.key, {
        state: 'failed',
        fault: thrown,
        ...(await this.#takeBack(part)),
      });
    }
  }

  /**
   * Deletes a part a failed drop made, and answers what is left of it when
   * that fails too, so the row can say so rather than claim nothing is left.
   */
  async #takeBack(part: PartDetail): Promise<Partial<DropJob>> {
    try {
      const left = await this.#catalogue.deletePartQuietly(part.id);

      return left === undefined
        ? {}
        : { left: 'files', leftIn: part.dir, leftFault: left };
    } catch (thrown) {
      return { left: 'part', part: part.id, leftFault: thrown };
    }
  }

  #mark(key: string, change: Partial<DropJob>): void {
    this.#jobs = this.#jobs.map((job) =>
      job.key === key ? { ...job, ...change } : job,
    );
  }
}
