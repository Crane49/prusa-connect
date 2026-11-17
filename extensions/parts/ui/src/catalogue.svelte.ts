// What the Parts page reads and what it writes through.
//
// A class, because it is state the whole page shares across reads and writes
// (svelte-5.md). The two services come in through the constructor, which is
// what lets a test drive it without a network.
//
// Three reads, each with its own loading, fault and answer, because a page
// that could not read one of them still draws the other two (svelte-data.md):
//
//   the overview  the folder tree, the tags, the room left. Read once, and
//                 again after a write that changes a count
//   the listing   one page of the parts of the folder or the search on screen
//   the part      the one part open on screen, with its versions and comments
//
// Every collection the server owns is `$state.raw` and is replaced whole on
// every read. The folders of the overview are indexed once per read, by id and
// by parent, so a tree of thousands of folders is walked by lookups and never
// by filtering the whole list for each row. A fault is kept as the value that
// was thrown and never as a sentence, because the code is what a translation
// is keyed by (frontend-i18n.md rule 5).
//
// Two lanes of writes. A dialog's write marks the catalogue busy and keeps
// what it was refused as `fault`, which the dialog or the page draws. A write
// made away from any dialog, such as a part made from files dropped onto a
// folder, goes through the methods named `…Quietly`: they throw what they are
// refused and touch neither, so a drop finishing in the background never
// re-enables a dialog still waiting for its own answer or clears a note the
// person has not read yet.
import type { PrintDetails, ServerStorage } from '@prusa/connect-ui/data';
import { PAGE_SIZES, type PageSize } from '@prusa/connect-ui/primitives';

import type { Allowed, FilesService } from './files';
import { indexOf, NO_FOLDERS } from './folder-index';
import type { Spot } from './navigation.svelte';
import type {
  Folder,
  FolderDraft,
  Overview,
  PartDetail,
  PartDraft,
  PartFile,
  PartQuery,
  PartRow,
  PartsService,
  Removed,
  Tag,
} from './parts';

/** How many parts one page of the listing holds, before the person picks. */
export const PAGE_SIZE: PageSize = 50;

/** The orders a listing may be in. */
export type Order = 'name' | 'changed';

/** What a person may do before the core has answered: nothing. */
const NOTHING: Allowed = {
  read: false,
  write: false,
  upload: false,
  files: false,
};

/** The key one listing is read under, so the same listing is not read twice. */
const listingKey = (
  spot: Spot,
  order: Order,
  offset: number,
  limit: number,
): string =>
  JSON.stringify([spot.folder, spot.q, spot.tag, order, offset, limit]);

/**
 * Which folders a listing reads. A search runs over the folder on screen and
 * every folder under it, and over the whole catalogue from the top. With no
 * search it is the one folder on screen.
 */
const scopeOf = (spot: Spot): PartQuery => {
  if (spot.q === '' && spot.tag === '') return { folder: spot.folder };

  return spot.folder === 0 ? {} : { folder: spot.folder, deep: true };
};

export class Catalogue {
  readonly #parts: PartsService;
  readonly #files: FilesService;
  /** Who the host says is signed in. It is read once and never changes. */
  readonly who: string;

  #allowed = $state.raw<Allowed>(NOTHING);
  #allowedRead = $state(false);
  #storages = $state.raw<readonly ServerStorage[]>([]);

  #overview = $state.raw<Overview | undefined>(undefined);
  #overviewFault = $state.raw<unknown>(undefined);
  #overviewAbort: AbortController | undefined;
  #index = $derived(indexOf(this.#overview?.folders ?? []));
  #branches = $derived(
    (this.#overview?.folders ?? [])
      .filter((folder) => this.#index.children.has(folder.id))
      .map((folder) => folder.id),
  );

  #rows = $state.raw<readonly PartRow[]>([]);
  #matched = $state(0);
  #held = $state(0);
  #listed = $state(false);
  #listing = $state(false);
  #listingFault = $state.raw<unknown>(undefined);
  #listedKey = '';
  #listingStale = false;
  #listingAbort: AbortController | undefined;
  #order = $state<Order>('name');
  #offset = $state(0);
  #limit = $state<PageSize>(PAGE_SIZE);
  #spot: Spot | undefined;

  #detail = $state.raw<PartDetail | undefined>(undefined);
  #detailId = $state<number | undefined>(undefined);
  #detailLoading = $state(false);
  #detailFault = $state.raw<unknown>(undefined);
  #detailAbort: AbortController | undefined;

  /** How many dialog writes are in flight. Background writes are not counted. */
  #writing = $state(0);
  #fault = $state.raw<unknown>(undefined);
  #described = $state.raw<Readonly<Record<string, PrintDetails | null>>>({});

  /** The first read of the frame, so two components asking make one request. */
  #framing: Promise<void> | undefined;

  constructor(parts: PartsService, files: FilesService, who: string) {
    this.#parts = parts;
    this.#files = files;
    this.who = who;
  }

  /** What the person may do. Nothing until the core has answered. */
  get allowed(): Allowed {
    return this.#allowed;
  }

  /** True once the core answered what the person may do. */
  get allowedRead(): boolean {
    return this.#allowedRead;
  }

  /** The folder tree, the tags and the room left. Read-only by design. */
  get overview(): Overview | undefined {
    return this.#overview;
  }

  /** Why the overview could not be read. */
  get overviewFault(): unknown {
    return this.#overviewFault;
  }

  /** Every folder, ordered by parent and then by name. */
  get folders(): readonly Folder[] {
    return this.#overview?.folders ?? [];
  }

  /** Every tag in use, the most used first. */
  get tags(): readonly Tag[] {
    return this.#overview?.tags ?? [];
  }

  /** The storage new files go into, as the core describes it. */
  get storage(): ServerStorage | undefined {
    const name = this.#overview?.storage;

    return this.#storages.find((row) => row.name === name);
  }

  /** The parts on the page of the listing. Read-only by design. */
  get rows(): readonly PartRow[] {
    return this.#rows;
  }

  /** How many parts the listing matched over every page. */
  get matched(): number {
    return this.#matched;
  }

  /** How many parts the catalogue holds. */
  get held(): number {
    return this.#held;
  }

  /** False until the first listing answered, which is not an empty listing. */
  get listed(): boolean {
    return this.#listed;
  }

  /** A listing is on its way. */
  get listing(): boolean {
    return this.#listing;
  }

  /** Why the listing could not be read. */
  get listingFault(): unknown {
    return this.#listingFault;
  }

  get order(): Order {
    return this.#order;
  }

  /** Changes the order and reads the first page again. */
  set order(next: Order) {
    if (next === this.#order) return;
    this.#order = next;
    this.#offset = 0;
    void this.#readListing();
  }

  /** How many parts one page holds. */
  get limit(): PageSize {
    return this.#limit;
  }

  /** Changes how many parts one page holds, and reads the first page again. */
  set limit(next: PageSize) {
    if (!PAGE_SIZES.includes(next) || next === this.#limit) return;
    this.#limit = next;
    this.#offset = 0;
    void this.#readListing();
  }

  /** Where the page of the listing starts. */
  get offset(): number {
    return this.#offset;
  }

  /** Moves to another page of the listing. */
  set offset(next: number) {
    if (!Number.isInteger(next) || next < 0 || next === this.#offset) return;
    this.#offset = next;
    void this.#readListing();
  }

  /** The part open on screen, once it has been read. Read-only by design. */
  get detail(): PartDetail | undefined {
    return this.#detail;
  }

  /** The id of the part the page asked for, whether or not it arrived. */
  get detailId(): number | undefined {
    return this.#detailId;
  }

  /** The open part is on its way. */
  get detailLoading(): boolean {
    return this.#detailLoading;
  }

  /** Why the open part could not be read. */
  get detailFault(): unknown {
    return this.#detailFault;
  }

  /**
   * A dialog's write is in flight. The page reports it on the control that
   * started it. A write made away from any dialog does not count.
   */
  get busy(): boolean {
    return this.#writing > 0;
  }

  /** What the last dialog write threw, and undefined when none did. */
  get fault(): unknown {
    return this.#fault;
  }

  /** Forgets the last write's fault, when the person closes the note. */
  dismiss(): void {
    this.#fault = undefined;
  }

  /** One folder by id. */
  folder(id: number): Folder | undefined {
    return this.#index.byId.get(id);
  }

  /** The folders directly inside one folder, and inside the top for 0. */
  childrenOf(id: number): readonly Folder[] {
    return this.#index.children.get(id) ?? NO_FOLDERS;
  }

  /** Every folder with folders inside it, the ones a "close all" closes. */
  get branches(): readonly number[] {
    return this.#branches;
  }

  /** The folders from the top down to one folder, that folder last. */
  trailOf(id: number): readonly Folder[] {
    const trail: Folder[] = [];
    // The tree is at most twelve deep, and a loop the server refused would
    // still end here.
    for (let at = this.folder(id); at !== undefined && trail.length < 16;) {
      trail.unshift(at);
      at = at.parent === 0 ? undefined : this.folder(at.parent);
    }

    return trail;
  }

  /**
   * Follows the place on screen: reads the frame once, the part when one is
   * open, and the listing when none is. A place already read is not read
   * again, so following the same place twice makes no request.
   */
  follow(spot: Spot): Promise<void> {
    const before = this.#spot;
    this.#spot = spot;

    const moved =
      before?.folder !== spot.folder ||
      before.q !== spot.q ||
      before.tag !== spot.tag;
    if (moved) this.#offset = 0;

    const reads: Promise<void>[] = [this.#frame()];

    if (spot.part === undefined) {
      this.#closeDetail();
      if (
        this.#listingStale ||
        listingKey(spot, this.#order, this.#offset, this.#limit) !==
          this.#listedKey
      ) {
        reads.push(this.#readListing());
      }
    } else if (spot.part !== this.#detailId) {
      reads.push(this.openPart(spot.part));
    }

    return Promise.all(reads).then(() => undefined);
  }

  /**
   * Reads the frame alone: the tree, the tags and what the person may do. A
   * dashboard card's settings read it to offer the folders and the tags, and
   * the page reads it too, so whichever comes first reads it for both.
   */
  frame(): Promise<void> {
    return this.#frame();
  }

  /** Reads everything on screen again: the frame, the listing, the open part. */
  async refresh(): Promise<void> {
    this.#framing = undefined;
    const reads: Promise<void>[] = [this.#frame()];
    if (this.#detailId === undefined) reads.push(this.#readListing());
    else reads.push(this.openPart(this.#detailId));
    await Promise.all(reads);
  }

  /**
   * Reads the listing on screen again after writes made outside a dialog,
   * such as files dropped onto a folder. With a part open, the listing is
   * read when the person goes back to it.
   */
  async relist(): Promise<void> {
    if (this.#spot === undefined || this.#spot.part !== undefined) return;
    await this.#readListing();
  }

  /** Reads one part, and drops a read of another one still in flight. */
  async openPart(id: number): Promise<void> {
    this.#detailAbort?.abort();
    const abort = new AbortController();
    this.#detailAbort = abort;

    if (this.#detailId !== id) this.#detail = undefined;
    this.#detailId = id;
    this.#detailLoading = true;
    this.#detailFault = undefined;

    try {
      const detail = await this.#parts.part(id, abort.signal);
      if (abort.signal.aborted) return;
      this.#detail = detail;
    } catch (thrown) {
      if (abort.signal.aborted) return;
      this.#detail = undefined;
      this.#detailFault = thrown;
    } finally {
      if (this.#detailAbort === abort) this.#detailLoading = false;
    }
  }

  /** What the slicer wrote into a print file, once it has been read. */
  described(file: PartFile): PrintDetails | undefined {
    return this.#described[`${file.storage}\u0000${file.path}`] ?? undefined;
  }

  /**
   * Reads what the slicer wrote into a print file, for the printer picker.
   * A file that carries nothing is recorded as such, so it is not asked again.
   */
  async describe(file: PartFile): Promise<void> {
    const key = `${file.storage}\u0000${file.path}`;
    if (key in this.#described) return;

    try {
      const details = await this.#files.describe(file.storage, file.path);
      this.#described = { ...this.#described, [key]: details ?? null };
    } catch {
      // The picker draws a file it knows nothing about; a refusal here costs
      // the details and not the print.
      this.#described = { ...this.#described, [key]: null };
    }
  }

  // ── Writes ───────────────────────────────────────────────────────────

  /** Makes one folder, and answers it, or undefined when it was refused. */
  async addFolder(draft: FolderDraft): Promise<Folder | undefined> {
    return await this.#write(async () => {
      const folder = await this.#parts.addFolder(draft);
      await this.#reframe();

      return folder;
    });
  }

  /** Renames, moves or describes one folder. */
  async changeFolder(id: number, draft: FolderDraft): Promise<boolean> {
    return (
      (await this.#write(async () => {
        await this.#parts.changeFolder(id, draft);
        await this.#reframe();

        return true;
      })) === true
    );
  }

  /** Deletes one empty folder. */
  async deleteFolder(id: number): Promise<boolean> {
    return (
      (await this.#write(async () => {
        await this.#parts.deleteFolder(id);
        await this.#reframe();

        return true;
      })) === true
    );
  }

  /** Makes one part, and answers it, or undefined when it was refused. */
  async addPart(draft: PartDraft): Promise<PartDetail | undefined> {
    return await this.#write(async () => await this.#makePart(draft));
  }

  /**
   * Changes the open part. The draft carries the revision the page read, so a
   * change somebody else made in between is refused rather than overwritten.
   */
  async changePart(id: number, draft: PartDraft): Promise<boolean> {
    const sent: PartDraft =
      this.#detail?.id === id && draft.revision === undefined
        ? { ...draft, revision: this.#detail.revision }
        : draft;

    return (
      (await this.#write(async () => {
        const part = await this.#parts.changePart(id, sent);
        if (this.#detailId === id) this.#detail = part;
        this.#listingStale = true;
        // A name, a folder or a tag changes what the frame counts.
        if (
          draft.folder !== undefined ||
          draft.tags !== undefined ||
          draft.name !== undefined
        ) {
          await this.#reframe();
        }

        return true;
      })) === true
    );
  }

  /**
   * Deletes one part, then the files it had. The module answers where the
   * part's files are in the storage, and the page deletes them, which takes
   * write_storage for the person. A file that could not be deleted is left
   * in the storage, and the fault says so.
   */
  async deletePart(id: number): Promise<boolean> {
    return (
      (await this.#write(async () => {
        const left = await this.#removePart(id);
        if (left !== undefined) this.#fault = left;

        return true;
      })) === true
    );
  }

  /** Records one version of the open part from files already uploaded. */
  async addVersion(
    id: number,
    note: string,
    files: Parameters<PartsService['addVersion']>[2],
  ): Promise<boolean> {
    return (
      (await this.#write(async () => {
        await this.#recordVersion(id, note, files);

        return true;
      })) === true
    );
  }

  /** Rewrites the note of one version. */
  async changeVersion(
    id: number,
    number: number,
    note: string,
  ): Promise<boolean> {
    return (
      (await this.#write(async () => {
        const part = await this.#parts.changeVersion(id, number, note);
        if (this.#detailId === id) this.#detail = part;

        return true;
      })) === true
    );
  }

  /** Deletes one version, then the files nothing names any more. */
  async deleteVersion(id: number, number: number): Promise<boolean> {
    return (
      (await this.#write(async () => {
        const answer = await this.#parts.deleteVersion(id, number);
        if (this.#detailId === id) this.#detail = answer.part;
        this.#listingStale = true;
        const left = await this.#clean(answer.removed);
        if (left !== undefined) this.#fault = left;

        return true;
      })) === true
    );
  }

  /** Writes one comment on the open part. */
  async addComment(id: number, body: string): Promise<boolean> {
    return (
      (await this.#write(async () => {
        const comment = await this.#parts.addComment(id, body);
        const detail = this.#detail;
        if (detail?.id === id) {
          this.#detail = { ...detail, comments: [...detail.comments, comment] };
        }
        this.#listingStale = true;

        return true;
      })) === true
    );
  }

  /** Rewrites one comment the signed-in person wrote. */
  async changeComment(
    id: number,
    comment: number,
    body: string,
  ): Promise<boolean> {
    return (
      (await this.#write(async () => {
        const written = await this.#parts.changeComment(id, comment, body);
        const detail = this.#detail;
        if (detail?.id === id) {
          this.#detail = {
            ...detail,
            comments: detail.comments.map((row) =>
              row.id === comment ? written : row,
            ),
          };
        }

        return true;
      })) === true
    );
  }

  /** Deletes one comment the signed-in person wrote. */
  async deleteComment(id: number, comment: number): Promise<boolean> {
    return (
      (await this.#write(async () => {
        await this.#parts.deleteComment(id, comment);
        const detail = this.#detail;
        if (detail?.id === id) {
          this.#detail = {
            ...detail,
            comments: detail.comments.filter((row) => row.id !== comment),
          };
        }
        this.#listingStale = true;

        return true;
      })) === true
    );
  }

  // ── Writes away from any dialog ──────────────────────────────────────
  //
  // The same writes as above, for work that runs in the background. Each one
  // throws what it was refused, and none marks the catalogue busy or keeps a
  // fault: the caller draws the refusal where its own work is shown.

  /** Makes one part, and throws when it was refused. */
  async addPartQuietly(draft: PartDraft): Promise<PartDetail> {
    return await this.#makePart(draft);
  }

  /** Records one version from files already uploaded, and throws when it was refused. */
  async addVersionQuietly(
    id: number,
    note: string,
    files: Parameters<PartsService['addVersion']>[2],
  ): Promise<void> {
    await this.#recordVersion(id, note, files);
  }

  /**
   * Deletes one part, then its files. Throws when the part itself could not
   * be deleted, and answers what the storage threw for a file it left
   * behind, or undefined when every file went.
   */
  async deletePartQuietly(id: number): Promise<unknown> {
    return await this.#removePart(id);
  }

  async #makePart(draft: PartDraft): Promise<PartDetail> {
    const part = await this.#parts.addPart(draft);
    this.#listingStale = true;
    await this.#reframe();

    return part;
  }

  async #recordVersion(
    id: number,
    note: string,
    files: Parameters<PartsService['addVersion']>[2],
  ): Promise<void> {
    const part = await this.#parts.addVersion(id, note, files);
    if (this.#detailId === id) this.#detail = part;
    this.#listingStale = true;
    await this.#reframe();
  }

  /** Deletes one part and its files, and answers what a file left behind threw. */
  async #removePart(id: number): Promise<unknown> {
    const removed = await this.#parts.deletePart(id);
    if (this.#detailId === id) this.#closeDetail();
    this.#listingStale = true;
    await this.#reframe();

    return await this.#clean(removed);
  }

  // ── Reads ────────────────────────────────────────────────────────────

  /**
   * The frame: what the person may do, the storages, the overview. Read once;
   * a second call while the first is in flight is handed the first one's
   * promise (svelte-data.md).
   */
  #frame(): Promise<void> {
    this.#framing ??= Promise.all([
      this.#readAllowed(),
      this.#readStorages(),
      this.#readOverview(),
    ]).then(() => undefined);

    return this.#framing;
  }

  async #readAllowed(): Promise<void> {
    try {
      this.#allowed = await this.#files.allowed();
    } catch {
      // A core that cannot say what the person may do leaves every control
      // hidden. The routes still decide, so a hidden control costs nothing.
      this.#allowed = NOTHING;
    } finally {
      this.#allowedRead = true;
    }
  }

  async #readStorages(): Promise<void> {
    try {
      this.#storages = await this.#files.storages();
    } catch {
      // Without the list the page cannot say which extensions the storage
      // takes, and the storage itself still refuses one it does not.
      this.#storages = [];
    }
  }

  /**
   * Reads the overview, and drops a read of it still in flight. Writes read it
   * again one after another, and an older answer arriving last would put an
   * older tree back on screen (svelte-data.md).
   */
  async #readOverview(): Promise<void> {
    this.#overviewAbort?.abort();
    const abort = new AbortController();
    this.#overviewAbort = abort;

    try {
      const overview = await this.#parts.overview(abort.signal);
      if (abort.signal.aborted) return;
      this.#overview = overview;
      this.#overviewFault = undefined;
    } catch (thrown) {
      if (abort.signal.aborted) return;
      this.#overviewFault = thrown;
    }
  }

  /** Reads the overview again after a write that changed what it counts. */
  async #reframe(): Promise<void> {
    await this.#readOverview();
  }

  async #readListing(): Promise<void> {
    const spot = this.#spot;
    if (spot === undefined) return;

    this.#listingAbort?.abort();
    const abort = new AbortController();
    this.#listingAbort = abort;

    const key = listingKey(spot, this.#order, this.#offset, this.#limit);
    this.#listedKey = key;
    this.#listingStale = false;
    this.#listing = true;
    this.#listingFault = undefined;

    try {
      const page = await this.#parts.list(
        {
          ...scopeOf(spot),
          ...(spot.q === '' ? {} : { q: spot.q }),
          ...(spot.tag === '' ? {} : { tag: spot.tag }),
          order: this.#order,
          offset: this.#offset,
          limit: this.#limit,
        },
        abort.signal,
      );
      if (abort.signal.aborted) return;
      this.#rows = page.parts;
      this.#matched = page.matched;
      this.#held = page.held;
      this.#listed = true;
    } catch (thrown) {
      if (abort.signal.aborted) return;
      this.#listingFault = thrown;
      this.#listedKey = '';
    } finally {
      if (this.#listingAbort === abort) this.#listing = false;
    }
  }

  #closeDetail(): void {
    this.#detailAbort?.abort();
    this.#detail = undefined;
    this.#detailId = undefined;
    this.#detailFault = undefined;
    this.#detailLoading = false;
  }

  /**
   * Deletes from the storage what a delete left behind. A deleted part names
   * every folder its files were kept in, each in its own storage, and each
   * folder goes whole. A deleted version names the files no other version
   * holds, and each goes on its own. Each target is its own request, and one
   * that fails does not stop the others. What the first failure threw is
   * answered, so the caller tells the person that a file is left over.
   */
  async #clean(removed: Removed): Promise<unknown> {
    const dirs = removed.dirs;
    // A recorded file outside every folder named still goes, on its own.
    const inside = (file: { storage: string; path: string }): boolean =>
      dirs.some(
        (row) =>
          row.storage === file.storage && file.path.startsWith(`${row.dir}/`),
      );
    const targets = [
      ...dirs.map((row) => ({ storage: row.storage, path: row.dir })),
      ...removed.files
        .filter((file) => !inside(file))
        .map((file) => ({ storage: file.storage, path: file.path })),
    ];

    const results = await Promise.allSettled(
      targets.map(({ storage, path }) => this.#files.remove(storage, path)),
    );

    const failed = results.find((result) => result.status === 'rejected');

    return failed?.reason;
  }

  /**
   * One dialog write: busy while it runs, the fault kept when it is refused,
   * and what it answered as the result. Every dialog write is the same three
   * steps, so they are written once here. Writes are counted, so one that
   * ends while another is still on its way leaves the catalogue busy.
   */
  async #write<T>(send: () => Promise<T>): Promise<T | undefined> {
    this.#writing += 1;
    this.#fault = undefined;

    try {
      return await send();
    } catch (thrown) {
      this.#fault = thrown;

      return undefined;
    } finally {
      this.#writing -= 1;
    }
  }
}
