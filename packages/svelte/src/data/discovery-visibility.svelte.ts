// Whether this server is hidden from the local network, for the settings
// page. A hidden server announces nothing, so the other printers do not list
// it. It still serves its page and its API.
import { type DataFault, faultOf } from './fault';
import {
  type DiscoveryClient,
  readVisibility,
  type Visibility,
  writeVisibility,
} from './services/discovery';

/**
 * A shared store, so a class (svelte-5.md). The client comes in through the
 * constructor, which lets a test pass a fake.
 *
 * The setting is the server's. The store holds what the server last answered,
 * and a change is shown only once the server has applied it.
 */
export class DiscoveryVisibility {
  readonly #client: DiscoveryClient;

  #answer = $state.raw<Visibility | undefined>(undefined);
  #loading = $state(false);
  #saving = $state(false);
  #error = $state<DataFault | undefined>(undefined);
  #flight: Promise<void> | undefined;
  // Counts the resets, so an answer to a request sent before one records
  // nothing.
  #resets = 0;
  // Counts the writes, so a read sent before one cannot draw the switch back.
  #writes = 0;

  constructor(client: DiscoveryClient) {
    this.#client = client;
  }

  /** True when the server announces nothing. False until the first answer. */
  get hidden(): boolean {
    return this.#answer?.hidden ?? false;
  }

  /** What the announcer does: announcing, hidden, off or failed. */
  get announcement(): string | undefined {
    return this.#answer?.announcement;
  }

  /**
   * What the server announces, or would announce once shown again: its
   * name, host, port, service names and addresses. Undefined until the first
   * answer.
   */
  get described(): Visibility | undefined {
    return this.#answer;
  }

  /** True once a read has answered, which tells "visible" from "not read yet". */
  get loaded(): boolean {
    return this.#answer !== undefined;
  }

  get loading(): boolean {
    return this.#loading;
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

  /**
   * Forgets the setting. The app calls it when the person signed in changes,
   * so the next person never sees what the last one could read.
   */
  reset(): void {
    this.#resets += 1;
    this.#flight = undefined;
    this.#answer = undefined;
    this.#loading = false;
    this.#saving = false;
    this.#error = undefined;
  }

  /**
   * Hides the server, or shows it again. A change asked for while another is
   * on its way is dropped, and a refusal keeps the setting the server holds.
   */
  async setHidden(hidden: boolean): Promise<void> {
    if (this.#saving) return;

    const resets = this.#resets;
    this.#writes += 1;
    this.#saving = true;
    this.#error = undefined;
    try {
      const answer = await writeVisibility(this.#client, hidden);
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
    this.#loading = true;
    try {
      const answer = await readVisibility(this.#client);
      if (resets !== this.#resets || writes !== this.#writes) return;
      this.#answer = answer;
      this.#error = undefined;
    } catch (thrown) {
      if (resets !== this.#resets) return;
      this.#error = faultOf(thrown);
    } finally {
      if (resets === this.#resets) this.#loading = false;
    }
  }
}
