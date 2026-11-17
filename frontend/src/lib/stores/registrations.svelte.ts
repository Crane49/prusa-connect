// The devices waiting to be let onto this server, read through the SDK and
// kept current by the event stream.
//
// It lives here rather than in the component library because the library holds
// the screen and the screen takes a port (ADR-20260917-j7s rule 1). A second product
// on another core hands in an object of its own shape and never this class.
import type { components, PrusaConnect, StreamEvent } from '@prusa/connect';
import {
  type RefusalReason,
  registrationChangeOf,
} from '@prusa/connect-ui/data';
import type { PendingPrinter } from '@prusa/connect-ui/shell';

type Registration = components['schemas']['Registration'];

/**
 * What this store needs of the SDK. Naming the three operations is what lets a
 * test hand it a fake without writing out every operation the core declares
 * (svelte-data.md).
 */
export type RegistrationsClient = Pick<
  PrusaConnect,
  'approveRegistration' | 'listPendingRegistrations' | 'rejectRegistration'
>;

/** A source of stream events: the fleet, which the app's one stream feeds. */
export interface RegistrationSource {
  subscribe(listener: (event: StreamEvent) => void): () => void;
}

/** A printer something let in, as this page learned of it. */
export interface Approval {
  readonly code: string;
  /** The device the approval bound, which the printer arrives under. */
  readonly deviceId: string | undefined;
  readonly model: string | undefined;
  readonly serial: string | undefined;
  readonly firmware: string | undefined;
  /** When this page learned of it, in milliseconds since the epoch. */
  readonly at: number;
}

/** A printer a connector turned away at the door. */
export interface Refusal {
  readonly reason: RefusalReason;
  readonly admission: 'approval' | 'automatic';
  readonly model: string | undefined;
  readonly firmware: string | undefined;
  /** The SHA-256 of the identity it presented, which tells two apart. */
  readonly digest: string | undefined;
  readonly at: number;
}

const CONTINUITY_LOST = 'com.prusa.connect.stream.continuity.lost';

// The decisions and refusals a page shows are the recent ones. Older ones are
// history, which the bell and the device pages already hold.
const KEPT = 20;

/**
 * One waiting device as the screen draws it. The record carries no timestamp,
 * so neither does the row: a "waiting since" would be a fact this server does
 * not have.
 */
const rowOf = (row: Registration): PendingPrinter => ({
  code: row.code,
  fingerprint: row.fingerprint,
  model: row.model,
  firmware: row.firmware,
  serial: row.serial,
});

export class Registrations {
  readonly #client: RegistrationsClient;
  readonly #now: () => number;
  // The server owns the whole list, so it is replaced as a whole and never
  // edited row by row (svelte-5.md).
  #pending = $state.raw<readonly PendingPrinter[]>([]);
  #approvals = $state.raw<readonly Approval[]>([]);
  #refusals = $state.raw<readonly Refusal[]>([]);
  #loading = $state(false);
  #loaded = $state(false);
  #error = $state<unknown>(undefined);
  // One flight per piece of data: a second call while one is in the air waits
  // on the first rather than asking again (svelte-data.md).
  #flight: Promise<void> | undefined;
  // A frame that arrived while a read was in flight. The answer may predate
  // it, so the list is read once more.
  #stale = false;
  // Counts the resets. A request that started before a reset records nothing.
  #resets = 0;

  constructor(client: RegistrationsClient, now: () => number = Date.now) {
    this.#client = client;
    this.#now = now;
  }

  get pending(): readonly PendingPrinter[] {
    return this.#pending;
  }

  /**
   * The printers let in while this page was open, newest first. Read-only by
   * design: an approval and the stream are what add to it.
   */
  get approvals(): readonly Approval[] {
    return this.#approvals;
  }

  /**
   * The printers turned away while this page was open, newest first. A code
   * announced or approved after them outdates them all: a printer the person
   * followed the hint on stops presenting what it was refused for, and one
   * that is still refused is refused again on its next call.
   */
  get refusals(): readonly Refusal[] {
    return this.#refusals;
  }

  get loading(): boolean {
    return this.#loading;
  }

  /** True once a read has answered, whether it found rows or none. */
  get loaded(): boolean {
    return this.#loaded;
  }

  /** The last refusal, as it was thrown, so a screen renders it by its code. */
  get error(): unknown {
    return this.#error;
  }

  load(): Promise<void> {
    this.#flight ??= this.#readUntilCurrent().finally(() => {
      this.#flight = undefined;
    });

    return this.#flight;
  }

  /**
   * Applies one stream event. A code arriving adds a row, a decision removes
   * one, so the list is current without a reload. A lost place in the stream
   * means frames may be missing, so the list is read again.
   */
  apply(event: StreamEvent): void {
    if (event.type === CONTINUITY_LOST) {
      void this.load();

      return;
    }

    const change = registrationChangeOf(event);
    if (change === undefined) return;
    // A refusal changes no waiting code, so it does not outdate a read.
    if (this.#flight !== undefined && change.kind !== 'refused') {
      this.#stale = true;
    }

    switch (change.kind) {
      case 'announced': {
        const row: PendingPrinter = {
          code: change.code,
          fingerprint: change.fingerprint,
          model: change.model,
          firmware: change.firmware,
          serial: change.serial,
        };
        const others = this.#pending.filter((one) => one.code !== row.code);
        this.#pending = [...others, row];
        this.#refusals = [];
        break;
      }
      case 'approved':
        this.#drop(change.code);
        this.#approved({
          code: change.code,
          deviceId: change.deviceId,
          model: change.model,
          serial: change.serial,
          firmware: change.firmware,
          at: this.#now(),
        });
        break;
      case 'rejected':
        this.#drop(change.code);
        break;
      case 'refused':
        this.#refusals = [
          {
            reason: change.reason,
            admission: change.admission,
            model: change.model,
            firmware: change.firmware,
            digest: change.digest,
            at: this.#now(),
          },
          ...this.#refusals,
        ].slice(0, KEPT);
        break;
    }
  }

  /**
   * Hears every event the source applies. The app passes the fleet, which the
   * one stream feeds, so this costs no second stream (ADR-20260911-4p6 rule 23).
   */
  watch(source: RegistrationSource): () => void {
    return source.subscribe((event) => {
      this.apply(event);
    });
  }

  async approve(code: string): Promise<void> {
    const resets = this.#resets;
    const waiting = this.#pending.find((row) => row.code === code);
    try {
      const answer = await this.#client.approveRegistration({
        path: { code },
      });
      if (resets !== this.#resets) return;
      this.#error = undefined;
      this.#drop(code);
      this.#approved({
        code,
        deviceId: answer.device_id,
        model: answer.model ?? waiting?.model,
        serial: answer.serial ?? waiting?.serial,
        firmware: answer.firmware ?? waiting?.firmware,
        at: this.#now(),
      });
    } catch (thrown) {
      // A refused decision keeps the list as it was, so the person sees why
      // nothing changed.
      if (resets === this.#resets) this.#error = thrown;
    }
  }

  async reject(code: string): Promise<void> {
    const resets = this.#resets;
    try {
      await this.#client.rejectRegistration({ path: { code } });
      if (resets !== this.#resets) return;
      this.#error = undefined;
      this.#drop(code);
    } catch (thrown) {
      if (resets === this.#resets) this.#error = thrown;
    }
  }

  /**
   * Forgets every waiting device this store read. The app calls it when the
   * person signed in changes, so the next person never sees a list the last
   * one was allowed to read. An answer to a request made before the reset is
   * dropped when it arrives.
   *
   * A failed read keeps the rows of an earlier read. Those rows were read for
   * the same person, because a change of person resets the store first.
   */
  reset(): void {
    this.#resets += 1;
    this.#flight = undefined;
    this.#stale = false;
    this.#pending = [];
    this.#approvals = [];
    this.#refusals = [];
    this.#loading = false;
    this.#loaded = false;
    this.#error = undefined;
  }

  #drop(code: string): void {
    if (!this.#pending.some((row) => row.code === code)) return;
    this.#pending = this.#pending.filter((row) => row.code !== code);
  }

  /**
   * Records one approval. The answer to our own approval and the frame about
   * it describe one decision, so the second keeps the first's place and adds
   * only the device id the first lacked.
   */
  #approved(approval: Approval): void {
    const earlier = this.#approvals.find((one) => one.code === approval.code);
    if (earlier !== undefined) {
      if (earlier.deviceId !== undefined || approval.deviceId === undefined) {
        return;
      }
      this.#approvals = this.#approvals.map((one) =>
        one === earlier ? { ...earlier, deviceId: approval.deviceId } : one,
      );

      return;
    }
    this.#approvals = [approval, ...this.#approvals].slice(0, KEPT);
    this.#refusals = [];
  }

  async #readUntilCurrent(): Promise<void> {
    for (;;) {
      this.#stale = false;
      await this.#read();
      if (!this.#outdated()) return;
    }
  }

  /** Whether a frame arrived during the read that just answered. */
  #outdated(): boolean {
    return this.#stale && this.#error === undefined;
  }

  async #read(): Promise<void> {
    const resets = this.#resets;
    this.#loading = true;
    try {
      const answer = await this.#client.listPendingRegistrations();
      if (resets !== this.#resets) return;
      this.#pending = (answer.items ?? []).map(rowOf);
      this.#error = undefined;
      this.#loaded = true;
    } catch (thrown) {
      if (resets === this.#resets) this.#error = thrown;
    } finally {
      if (resets === this.#resets) this.#loading = false;
    }
  }
}
