// What an administrator changes about one printer's record: its name, its
// tags, and whether it is still part of the fleet.
import type { PrusaConnect } from '@prusa/connect';

import { type DataFault, faultOf } from './fault';

/** This is what the store needs of the SDK, and no more. */
export type DeviceAdministrationClient = Pick<
  PrusaConnect,
  'getDevice' | 'setDeviceName' | 'setDeviceTags' | 'retireDevice'
>;

/** Each act this store sends is a rename, a tag write or a retirement. */
export type AdministrationAct = 'rename' | 'tags' | 'retire';

type Tags = readonly string[];

/** One edit of the tag list, as a change to whatever list it is applied to. */
interface TagEdit {
  readonly change: (current: Tags) => Tags;
}

/** These options set how the store behaves, beyond its client. */
export interface DeviceAdministrationOptions {
  /**
   * Called after a name or a tag list is written, so the host can read the
   * printer's record again. The store waits for it before it reports done.
   */
  readonly afterWrite?: () => Promise<void>;
}

/**
 * A shared store for one printer's page, so a class (svelte-5.md).
 *
 * Every act waits for the server's answer. An error is kept by its code with
 * the act it stopped, so the control that sent it can say why.
 *
 * Tag writes go one at a time. Each one replaces the whole list on the
 * server, so each is built on the list the server holds when its turn comes.
 * The store reads the printer again just before it writes. A tag another
 * person added meanwhile then stays, and an edit queued behind a refused one
 * does not carry the refused tag.
 */
export class DeviceAdministration {
  readonly #client: DeviceAdministrationClient;
  readonly #deviceId: string;
  readonly #afterWrite: () => Promise<void>;

  // How many of each act are waiting on the server. Each act has its own
  // count, so one act ending never reports another as finished.
  #pending = $state<Record<AdministrationAct, number>>({
    rename: 0,
    tags: 0,
    retire: 0,
  });
  #error = $state<DataFault | undefined>(undefined);
  #failed = $state<AdministrationAct | undefined>(undefined);
  // The edits still waiting, and the last list the server answered. The list
  // shown is the waiting edits applied to that list.
  #tagEdits = $state.raw<readonly TagEdit[]>([]);
  #tagBase = $state.raw<Tags | undefined>(undefined);
  #holdingTags = $state(false);
  #tagQueue: Promise<unknown> = Promise.resolve();
  #tagWrites = 0;
  // What the writes since the queue was last empty have done.
  #batchWrote = false;
  #batchFault: DataFault | undefined;

  constructor(
    client: DeviceAdministrationClient,
    deviceId: string,
    options: DeviceAdministrationOptions = {},
  ) {
    this.#client = client;
    this.#deviceId = deviceId;
    this.#afterWrite = options.afterWrite ?? (() => Promise.resolve());
  }

  /** The printer this store is about. Read-only by design. */
  get deviceId(): string {
    return this.#deviceId;
  }

  /** Whether this act is waiting on the server. */
  pending(act: AdministrationAct): boolean {
    return this.#pending[act] > 0;
  }

  /** The last error, kept by its code. */
  get error(): DataFault | undefined {
    return this.#error;
  }

  /** Which act the last error stopped. */
  get failed(): AdministrationAct | undefined {
    return this.#failed;
  }

  /**
   * The tags to draw. While an edit is on its way this is the list the
   * waiting edits leave, and otherwise it is the record the page holds.
   */
  tags(record: Tags): Tags {
    if (!this.#holdingTags) return record;

    return this.#tagEdits.reduce(
      (list, edit) => edit.change(list),
      this.#tagBase ?? record,
    );
  }

  /** Writes what an operator calls the printer. An empty name takes it off. */
  rename(name: string): Promise<boolean> {
    return this.#act('rename', async () => {
      await this.#client.setDeviceName({
        path: { device_id: this.#deviceId },
        body: { name },
      });
      await this.#afterWrite();
    });
  }

  /** Takes the printer out of the fleet. Its record and history are kept. */
  retire(): Promise<boolean> {
    return this.#act('retire', async () => {
      await this.#client.retireDevice({ path: { device_id: this.#deviceId } });
    });
  }

  /**
   * Applies one change to the tag list and writes the result. The change is
   * applied to the list the server holds when the write's turn comes, so it
   * says what to do and never which list to write.
   */
  editTags(change: (current: Tags) => Tags): Promise<boolean> {
    const edit: TagEdit = { change };
    this.#holdingTags = true;
    this.#tagEdits = [...this.#tagEdits, edit];
    this.#tagWrites += 1;
    // The edit is pending from now, while it waits behind the writes before
    // it, until the record is read again.
    this.#pending.tags += 1;

    const write = this.#tagQueue.then(() =>
      this.#send('tags', async () => {
        const device = await this.#client.getDevice({
          path: { device_id: this.#deviceId },
        });
        const held = device.tags ?? [];
        this.#tagBase = held;
        const next = change(held);
        const answer = await this.#client.setDeviceTags({
          path: { device_id: this.#deviceId },
          body: { tags: [...next] },
        });
        this.#tagBase = answer.tags ?? next;
      }),
    );
    this.#tagQueue = write;

    return write
      .then((ok) => {
        this.#tagEdits = this.#tagEdits.filter((waiting) => waiting !== edit);

        return this.#settleTags(ok);
      })
      .finally(() => {
        this.#pending.tags -= 1;
      });
  }

  // Runs as each tag write is answered. The last answer in the queue decides
  // what the card shows and whether the record is read again.
  async #settleTags(ok: boolean): Promise<boolean> {
    if (ok) this.#batchWrote = true;
    else this.#batchFault ??= this.#error;
    this.#tagWrites -= 1;
    if (this.#tagWrites > 0) return ok;

    // An error anywhere in the queue is what the card shows, and if any
    // write was taken the record is read again.
    const wrote = this.#batchWrote;
    const fault = this.#batchFault;
    this.#batchWrote = false;
    this.#batchFault = undefined;
    if (fault !== undefined) {
      this.#error = fault;
      this.#failed = 'tags';
    }
    if (wrote) await this.#afterWrite();

    // An edit made while the record was read again starts a new queue, and
    // its list stays on screen until that queue is empty too.
    if (this.#tagWrites === 0) {
      this.#holdingTags = false;
      this.#tagBase = undefined;
    }

    return ok;
  }

  async #act(
    act: AdministrationAct,
    send: () => Promise<void>,
  ): Promise<boolean> {
    this.#pending[act] += 1;
    try {
      return await this.#send(act, send);
    } finally {
      this.#pending[act] -= 1;
    }
  }

  // Sends one act and keeps its error by code. The caller counts it pending.
  async #send(
    act: AdministrationAct,
    send: () => Promise<void>,
  ): Promise<boolean> {
    try {
      await send();
      this.#error = undefined;
      this.#failed = undefined;

      return true;
    } catch (thrown) {
      this.#error = faultOf(thrown);
      this.#failed = act;

      return false;
    }
  }
}
