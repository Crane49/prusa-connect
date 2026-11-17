// Whether this server answers the time on the local network, for the settings
// page. The switch is the server's, and a change is shown once applied.
import { type DataFault, faultOf } from './fault';
import {
  readTimeServer,
  type TimeServerClient,
  type TimeServerState,
  writeTimeServer,
} from './services/discovery';

/** A shared store, so a class (svelte-5.md). */
export class TimeServerSwitch {
  readonly #client: TimeServerClient;

  #answer = $state.raw<TimeServerState | undefined>(undefined);
  #saving = $state(false);
  #error = $state<DataFault | undefined>(undefined);
  #flight: Promise<void> | undefined;
  // Counts the resets, so an answer to a request sent before one records
  // nothing.
  #resets = 0;
  // Counts the writes, so a read sent before one cannot draw the switch back.
  #writes = 0;

  constructor(client: TimeServerClient) {
    this.#client = client;
  }

  /** True when the switch is on. False until the first answer. */
  get enabled(): boolean {
    return this.#answer?.enabled ?? false;
  }

  /** What the time server does: serving, off or failed. */
  get state(): string | undefined {
    return this.#answer?.state;
  }

  /** Why the time server failed, as the operating system said it. */
  get reason(): string | undefined {
    return this.#answer?.reason;
  }

  /** What the failure was, when known: permission or in_use. */
  get cause(): string | undefined {
    return this.#answer?.cause;
  }

  /** The UDP port a device asks for the time on. */
  get port(): number | undefined {
    return this.#answer?.port;
  }

  /** True once a read has answered. */
  get loaded(): boolean {
    return this.#answer !== undefined;
  }

  /** True while a change is on its way to the server. */
  get saving(): boolean {
    return this.#saving;
  }

  get error(): DataFault | undefined {
    return this.#error;
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

  /** Forgets the switch, when the person signed in changes. */
  reset(): void {
    this.#resets += 1;
    this.#flight = undefined;
    this.#answer = undefined;
    this.#saving = false;
    this.#error = undefined;
  }

  /**
   * Starts or stops the time server. A change asked for while another is on
   * its way is dropped, and a refusal keeps the switch the server holds.
   */
  async setEnabled(enabled: boolean): Promise<void> {
    if (this.#saving) return;

    const resets = this.#resets;
    this.#writes += 1;
    this.#saving = true;
    this.#error = undefined;
    try {
      const answer = await writeTimeServer(this.#client, enabled);
      if (resets === this.#resets) this.#answer = answer;
    } catch (thrown) {
      if (resets === this.#resets) this.#error = faultOf(thrown);
    } finally {
      if (resets === this.#resets) this.#saving = false;
    }
  }

  async #read(): Promise<void> {
    const resets = this.#resets;
    const writes = this.#writes;
    try {
      const answer = await readTimeServer(this.#client);
      if (resets !== this.#resets || writes !== this.#writes) return;
      this.#answer = answer;
      this.#error = undefined;
    } catch (thrown) {
      if (resets === this.#resets) this.#error = faultOf(thrown);
    }
  }
}
