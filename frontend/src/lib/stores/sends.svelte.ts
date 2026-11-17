// The files on their way to a printer. This tab's own files are uploaded to
// this server first, then sent from there to the printer, and printed there
// when the person asked for that. A file the person
// keeps is saved into a storage folder they chose. Any other file goes up as
// a temporary copy, which the server deletes once it is sent.
//
// Beside them it draws every other file the server is sending to a printer
// the person may read, whoever started it and from whichever browser, so two
// people looking at the same fleet see the same files on their way.
//
// One store for the app, so a file dropped on a printer page keeps going
// when the person moves to another page, and the shell can draw every file
// on its way wherever the person is.
import type {
  RunningTransferRow,
  RunningTransfers,
  PrintEnd,
  SavedObject,
  Storage,
  TransferMoved,
  Transfers,
} from '@prusa/connect-ui/data';
import { runningTransferOf } from '@prusa/connect-ui/data';

/** What this store reads of the Storage store. */
export type SendsStorage = Pick<
  Storage,
  'upload' | 'uploads' | 'cancelUpload' | 'uploadTemporary'
>;

/** What this store reads of one printer's Transfers store. */
export type SendsTransfers = Pick<
  Transfers,
  'send' | 'print' | 'error' | 'waiting'
>;

/** What this store reads of the store of every running transfer. */
export type SendsRunning = Pick<
  RunningTransfers,
  | 'rows'
  | 'progress'
  | 'landedAt'
  | 'awaitLanding'
  | 'forget'
  | 'stop'
  | 'stopping'
>;

/**
 * Where one file still going somewhere is: on its way to this server, or on
 * its way from this server to the printer.
 */
export type SendStep = 'uploading' | 'sending';

/**
 * What a send ended with. A job ended by `reset()` answers `'cancelled'`.
 * `'taken'` is a printer that holds a file of that name: the printer's page
 * asks the person whether to replace it. `'refused'` is a file that landed
 * and that the printer would not start: its reason is the printer store's
 * error.
 */
export type SendOutcome = 'done' | 'failed' | 'cancelled' | 'taken' | 'refused';

export interface SendRow {
  /** Stable while the row is drawn, and what `cancel` takes. */
  readonly id: string;
  readonly deviceId: string;
  /** The printer's name. */
  readonly printerName: string;
  readonly name: string;
  readonly step: SendStep;
  /** From 0 to 1 within the current step. Null while the length is unknown. */
  readonly progress: number | null;
  /** True when the signed-in person started it, in this tab or another. */
  readonly mine: boolean;
  /** Who started it, such as `user:ada`, when the server names the person. */
  readonly startedBy: string | undefined;
  /** True when the person may stop it, which `cancel` then does. */
  readonly canCancel: boolean;
  /** True while a stop asked for has not answered. */
  readonly stopping: boolean;
}

interface Job {
  readonly id: number;
  readonly deviceId: string;
  readonly printerName: string;
  readonly storage: string;
  readonly name: string;
  /**
   * Where the file is in the storage. For a kept file it is known before the
   * upload; for a temporary copy the server answers it, and it is empty
   * until then.
   */
  readonly source: string;
  /** Whether the file is saved in the storage, or only a temporary copy. */
  readonly kept: boolean;
  readonly destination: string;
  /** True when the printer starts the file once it lands. */
  readonly print: boolean;
  readonly step: SendStep;
  /** The bytes of a temporary copy sent so far, and of the whole file. */
  readonly sent: number;
  readonly total: number;
}

const nameOf = (path: string): string => path.split('/').pop() ?? path;

/** What a job's transfer moves, as the store of running transfers names it. */
const movedOf = (job: Job): TransferMoved => ({
  deviceId: job.deviceId,
  storage: job.storage,
  source: job.source,
  path: job.destination,
});

/** A name inside a folder, where the empty folder is the storage's root. */
const joinPath = (folder: string, name: string): string =>
  folder === '' ? name : `${folder}/${name}`;

/**
 * A shared store, so a class (svelte-5.md). Its inputs come in through the
 * constructor, which lets a test pass fakes.
 *
 * It holds where each file this tab is sending is in the chain. The bytes
 * counted come from the Storage store while a file goes up, and from the
 * store of every running transfer while it goes to the printer, so they are
 * never copied here.
 *
 * The relay from this server to the printer runs on without the page, so a
 * reload loses the tab's jobs and not the relay. The store of every running
 * transfer reads it again from the server, so a file still on its way after
 * a reload is drawn again, in this tab and in every other.
 */
export class Sends {
  readonly #storage: SendsStorage;
  readonly #transfers: (deviceId: string) => SendsTransfers;
  readonly #running: SendsRunning;
  #jobs = $state.raw<readonly Job[]>([]);
  // The upload running for each storage and name, with the file it carries.
  // Every printer page uploads into the same storage under the file's own
  // name, and the Storage store aborts an upload when a second one of the
  // same key starts. So a second send of the same file still going up waits
  // for the same upload. A different file of that name is uploaded, and the
  // send that waited for the older upload ends cancelled. It is not reactive,
  // because nothing draws it.
  // eslint-disable-next-line svelte/prefer-svelte-reactivity
  readonly #flights = new Map<
    string,
    { file: File; flight: Promise<SavedObject | undefined> }
  >();
  // The way to stop each temporary copy still going up, by job. A temporary
  // copy is never shared: the server deletes it after one send, so a second
  // printer gets a copy of its own. It is not reactive, because nothing
  // draws it.
  // eslint-disable-next-line svelte/prefer-svelte-reactivity
  readonly #aborts = new Map<number, AbortController>();
  #next = 0;
  // Counts the resets. A chain that started before a reset records nothing.
  #resets = 0;

  constructor(
    storage: SendsStorage,
    transfers: (deviceId: string) => SendsTransfers,
    running: SendsRunning,
  ) {
    this.#storage = storage;
    this.#transfers = transfers;
    this.#running = running;
  }

  /**
   * Every file still on its way to this server or to a printer: this tab's
   * own sends, newest first, then every other transfer the server is running
   * to a printer the person may read, newest first.
   */
  readonly rows: readonly SendRow[] = $derived.by(() => {
    // A transfer one of the jobs is waiting for is drawn as that job. The set
    // lives for this one computation, so nothing reads it reactively.
    // eslint-disable-next-line svelte/prefer-svelte-reactivity
    const claimed = new Set<string>();
    const own = this.#jobs.map((job): SendRow => {
      const relay = job.step === 'sending' ? this.#transferOf(job) : undefined;
      if (relay !== undefined) claimed.add(relay.transferId);

      return {
        id: `job:${String(job.id)}`,
        deviceId: job.deviceId,
        printerName: job.printerName,
        name: job.name,
        step: job.step,
        progress: this.#progressOf(job, relay),
        mine: true,
        startedBy: undefined,
        canCancel: job.step === 'uploading' || relay?.canStop === true,
        stopping:
          relay !== undefined && this.#running.stopping(relay.transferId),
      };
    });

    const relayed = this.#running.rows
      .filter((row) => !claimed.has(row.transferId))
      .map((row): SendRow => ({
        id: `transfer:${row.transferId}`,
        deviceId: row.deviceId,
        printerName: row.printerName,
        name: nameOf(row.path),
        step: 'sending',
        progress: this.#running.progress(row.transferId),
        mine: row.mine,
        startedBy: row.startedBy,
        canCancel: row.canStop,
        stopping: this.#running.stopping(row.transferId),
      }));

    return [...own, ...relayed];
  });

  /** True while a file is still on its way to this server or to a printer. */
  get active(): boolean {
    return this.rows.length > 0;
  }

  /**
   * True while the browser still holds a file's bytes. Closing the tab then
   * loses the file, which is what the shell asks the person about.
   */
  get uploading(): boolean {
    return this.#jobs.some((job) => job.step === 'uploading');
  }

  /**
   * Uploads one file into `storage`, then sends it to the printer at
   * `directory`. The job leaves `rows` the moment it ends, and the promise
   * resolves to the outcome it ended with. A job ended by `reset()` resolves
   * to `'cancelled'`.
   *
   * `keep` is the folder of `storage` the file is saved in, and the empty
   * string is its root. Without it the file goes up as a temporary copy,
   * which the server deletes once the file has landed or the transfer was
   * stopped, and keeps for a while after a failed send so the printer page
   * can send it again.
   *
   * The same kept file already going up to the same place is not uploaded
   * again: this send waits for that upload, then sends to its own printer. A
   * file is the same when its name, size and last change time match.
   *
   * `print` starts the file once it lands. The server sends it and starts it
   * in one request, so the print goes on when the page is closed. A start
   * the printer refuses answers `'refused'` and hands how it ended, with the
   * printer's fault, to `onRefused`.
   */
  async send(
    file: File,
    deviceId: string,
    printerName: string,
    storage: string,
    directory: string,
    {
      keep,
      print = false,
      onRefused,
    }: {
      keep?: string | undefined;
      print?: boolean;
      onRefused?: (ended: PrintEnd) => void;
    } = {},
  ): Promise<SendOutcome> {
    const resets = this.#resets;
    this.#next += 1;
    const kept = keep !== undefined;
    let job: Job = {
      id: this.#next,
      deviceId,
      printerName,
      storage,
      name: file.name,
      source: kept ? joinPath(keep, file.name) : '',
      kept,
      destination: `${directory}/${file.name}`,
      print,
      step: 'uploading',
      sent: 0,
      total: file.size,
    };
    this.#jobs = [job, ...this.#jobs];

    const transfers = this.#transfers(deviceId);
    const saved = kept
      ? await this.#upload(storage, job.source, file)
      : await this.#uploadTemporary(job, file);
    if (resets !== this.#resets) return 'cancelled';
    if (saved === 'cancelled' || saved === 'failed') {
      this.#end(job.id);

      return saved;
    }
    if (saved === undefined) {
      // The Storage row for this key may now belong to a later upload of
      // the same name from the Storage page, and it still reads "sending":
      // that later upload superseded this one, which counts as a cancel and
      // not a failure. Only a row the Storage store marked failed carries a
      // fault.
      const upload = this.#uploadOf(job);
      this.#end(job.id);

      return upload?.state === 'failed' ? 'failed' : 'cancelled';
    }

    job = { ...job, source: saved.path, step: 'sending' };
    // A landing of the same file to the same place from an earlier send is
    // not this one's.
    this.#running.awaitLanding(movedOf(job));
    this.#put(job);

    if (job.print) {
      const printed = await transfers.print(
        storage,
        job.source,
        job.destination,
      );
      if (resets !== this.#resets) return 'cancelled';
      this.#end(job.id);
      if (printed?.answer?.ok === true) return 'done';
      if (printed?.answer !== undefined) {
        onRefused?.(printed);

        return 'refused';
      }
    } else {
      const sent = await transfers.send(storage, job.source, job.destination);
      if (resets !== this.#resets) return 'cancelled';
      this.#end(job.id);
      if (sent !== undefined) return 'done';
    }

    // A name the printer holds is not a failure: the send waits in the
    // printer's store for the person's answer, and the printer page asks.
    const asked = transfers.waiting.some(
      (one) =>
        one.storage === storage &&
        one.source === job.source &&
        one.path === job.destination,
    );
    if (asked) return 'taken';

    // A transfer stopped on the printer page answers the send with an
    // error too. `sendFailure` in `internal/app/devicefiles.go` marks it
    // with the parameter `event` set to "stopped", which is not a failure.
    const stopped = transfers.error?.params.event === 'stopped';

    return stopped ? 'cancelled' : 'failed';
  }

  /**
   * Stops one file. A file still going up is cancelled in the browser. A file
   * on its way to the printer is stopped on the server, whoever started it,
   * when the person may stop it.
   *
   * Two sends that share one upload are stopped together: the upload is
   * cancelled, and both rows end cancelled.
   */
  cancel(id: string): void {
    const row = this.rows.find((held) => held.id === id);
    if (row?.canCancel !== true) return;

    const job = this.#jobs.find((held) => `job:${String(held.id)}` === id);
    if (job === undefined) {
      void this.#running.stop(id.slice('transfer:'.length));

      return;
    }
    if (job.step === 'sending') {
      const relay = this.#transferOf(job);
      if (relay !== undefined) void this.#running.stop(relay.transferId);

      return;
    }
    if (job.kept) this.#storage.cancelUpload(job.storage, job.source);
    else this.#aborts.get(job.id)?.abort();
  }

  /**
   * Forgets every row this tab holds. The app calls it when the person signed
   * in changes. The Storage store and the store of every running transfer
   * are reset with it, and the uploads on their way are aborted. A send
   * still waiting on one of them resolves to `'cancelled'`.
   */
  reset(): void {
    this.#resets += 1;
    this.#jobs = [];
    this.#flights.clear();
    for (const flight of this.#aborts.values()) flight.abort();
    this.#aborts.clear();
  }

  #upload(
    storage: string,
    path: string,
    file: File,
  ): Promise<SavedObject | undefined> {
    const key = `${storage}/${path}`;
    const running = this.#flights.get(key);
    if (
      running?.file.size === file.size &&
      running.file.lastModified === file.lastModified
    )
      return running.flight;

    const flight = this.#storage.upload(storage, path, file);
    this.#flights.set(key, { file, flight });
    const forget = (): void => {
      if (this.#flights.get(key)?.flight === flight) this.#flights.delete(key);
    };
    void flight.then(forget, forget);

    return flight;
  }

  // Sends one temporary copy up, with its progress kept on the job. It
  // answers what the server saved, or how the upload ended when it did not.
  async #uploadTemporary(
    job: Job,
    file: File,
  ): Promise<SavedObject | 'cancelled' | 'failed'> {
    const flight = new AbortController();
    this.#aborts.set(job.id, flight);
    try {
      const answer = await this.#storage.uploadTemporary(
        job.storage,
        file.name,
        file,
        (sent, total) => {
          const held = this.#jobs.find((one) => one.id === job.id);
          if (held?.step === 'uploading') this.#put({ ...held, sent, total });
        },
        flight.signal,
      );
      if ('saved' in answer) return answer.saved;

      return flight.signal.aborted ? 'cancelled' : 'failed';
    } finally {
      this.#aborts.delete(job.id);
    }
  }

  // The newest running transfer that moves this job's file to its printer.
  #transferOf(job: Job): RunningTransferRow | undefined {
    return runningTransferOf(this.#running.rows, {
      deviceId: job.deviceId,
      storage: job.storage,
      source: job.source,
      path: job.destination,
    });
  }

  #put(job: Job): void {
    this.#jobs = this.#jobs.map((held) => (held.id === job.id ? job : held));
  }

  /** Drops the job that just ended. */
  #end(id: number): void {
    // The landing a sending job waited on is no longer anybody's.
    const ended = this.#jobs.find((held) => held.id === id);
    if (ended?.step === 'sending') this.#running.forget(movedOf(ended));
    this.#jobs = this.#jobs.filter((held) => held.id !== id);
  }

  #uploadOf(job: Job) {
    return this.#storage.uploads.find(
      (row) => row.storage === job.storage && row.path === job.source,
    );
  }

  #progressOf(job: Job, relay: RunningTransferRow | undefined): number | null {
    if (job.step === 'uploading' && !job.kept) {
      return job.total > 0 ? Math.min(job.sent / job.total, 1) : null;
    }
    if (job.step === 'uploading') {
      const upload = this.#uploadOf(job);
      if (upload === undefined || upload.total <= 0) return null;

      return Math.min(upload.sent / upload.total, 1);
    }
    if (relay !== undefined) return this.#running.progress(relay.transferId);

    // The printer has the file and the server has not answered yet.
    const landed = this.#running.landedAt(movedOf(job));

    return landed ? 1 : null;
  }
}
