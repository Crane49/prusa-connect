// The grants one access list reads and changes: those written on one printer,
// or those written for one person. Which of the two is the read handed in.
import { SvelteSet } from 'svelte/reactivity';

import {
  type AccessChoice,
  type DirectRelation,
  directRelationOf,
  type OrgRole,
  planAccessChoice,
  planAccessOn,
} from '../domain/printer-access';
import { type DataFault, faultOf } from './fault';
import {
  applyAccessSteps,
  type Grant,
  type GrantsClient,
} from './services/grants';

const DEVICE_PREFIX = 'device:';

/** The row key of one person on one printer. */
export const accessKey = (deviceId: string, subject: string): string =>
  `${deviceId}\u0000${subject}`;

/**
 * A shared store, so a class (svelte-5.md). It is not optimistic: a change is
 * drawn once the server has applied it and the grants were read again, which
 * also puts a row back where the server holds it when a write is refused.
 */
export class AccessGrants {
  readonly #client: GrantsClient;
  readonly #read: (signal: AbortSignal) => Promise<readonly Grant[]>;

  #grants = $state.raw<readonly Grant[]>([]);
  #loading = $state(false);
  #loaded = $state(false);
  #error = $state<DataFault | undefined>(undefined);
  #busy = new SvelteSet<string>();
  #flight: Promise<void> | undefined;
  #abort: AbortController | undefined;

  constructor(
    client: GrantsClient,
    read: (
      client: GrantsClient,
      signal: AbortSignal,
    ) => Promise<readonly Grant[]>,
  ) {
    this.#client = client;
    this.#read = (signal) => read(client, signal);
  }

  /** Read-only by design: the grants are the server's (svelte-5.md). */
  get grants(): readonly Grant[] {
    return this.#grants;
  }

  get loading(): boolean {
    return this.#loading;
  }

  /** True once a read has answered, which tells "no grants" from "not read". */
  get loaded(): boolean {
    return this.#loaded;
  }

  /** The last refused read or write. */
  get error(): DataFault | undefined {
    return this.#error;
  }

  /** Whether a change to this person on this printer is in flight. */
  busy(deviceId: string, subject: string): boolean {
    return this.#busy.has(accessKey(deviceId, subject));
  }

  /** The relations this person holds on this printer, as written. */
  direct(deviceId: string, subject: string): DirectRelation[] {
    const object = DEVICE_PREFIX + deviceId;
    const out: DirectRelation[] = [];
    for (const grant of this.#grants) {
      if (grant.object !== object || grant.subject !== subject) continue;
      const relation = directRelationOf(grant.relation);
      if (relation !== undefined) out.push(relation);
    }

    return out;
  }

  /** A second call while one read is in flight is given that read. */
  load(): Promise<void> {
    if (this.#flight) return this.#flight;

    const flight = this.#readAll().finally(() => {
      if (this.#flight === flight) this.#flight = undefined;
    });
    this.#flight = flight;

    return flight;
  }

  /** Turns access on or off. */
  setOn(
    deviceId: string,
    subject: string,
    orgRole: OrgRole,
    on: boolean,
  ): Promise<void> {
    return this.#change(deviceId, subject, (direct) =>
      planAccessOn(orgRole, direct, on),
    );
  }

  /** Follows the organization role, or writes one printer role. */
  setChoice(
    deviceId: string,
    subject: string,
    choice: AccessChoice,
  ): Promise<void> {
    return this.#change(deviceId, subject, (direct) =>
      planAccessChoice(direct, choice),
    );
  }

  /** Stops a read in flight. The page calls it when it leaves. */
  close(): void {
    this.#abort?.abort();
    this.#abort = undefined;
  }

  async #change(
    deviceId: string,
    subject: string,
    plan: (direct: DirectRelation[]) => ReturnType<typeof planAccessOn>,
  ): Promise<void> {
    const key = accessKey(deviceId, subject);
    if (this.#busy.has(key)) return;

    const steps = plan(this.direct(deviceId, subject));
    if (steps.length === 0) return;

    this.#busy.add(key);
    this.#error = undefined;
    let refused: DataFault | undefined;
    try {
      await applyAccessSteps(this.#client, deviceId, subject, steps);
    } catch (thrown) {
      refused = faultOf(thrown);
    }
    // Read whether or not the writes landed: a sequence refused halfway has
    // still changed what the server holds.
    this.#flight = undefined;
    await this.load();
    if (refused !== undefined) this.#error = refused;
    this.#busy.delete(key);
  }

  async #readAll(): Promise<void> {
    this.#abort?.abort();
    const abort = new AbortController();
    this.#abort = abort;
    this.#loading = true;
    try {
      const grants = await this.#read(abort.signal);
      if (abort.signal.aborted) return;
      this.#grants = grants;
      this.#loaded = true;
      this.#error = undefined;
    } catch (thrown) {
      if (!abort.signal.aborted) this.#error = faultOf(thrown);
    } finally {
      if (this.#abort === abort) {
        this.#loading = false;
        this.#abort = undefined;
      }
    }
  }
}
