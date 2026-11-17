import { SvelteMap } from 'svelte/reactivity';

import {
  type DeviceIssueEntry,
  type DeviceIssuesClient,
  readDeviceIssues,
} from './services/device-issues';

/**
 * The reasons a printer can carry an issue, read once per session.
 *
 * A device record carries the reason token and no sentence, so a screen asks
 * this for the summary and the action. A reason the server does not list, or
 * a server that cannot answer, gives `undefined`, and the screen shows what
 * the machine itself said.
 *
 * A shared store, so a class (svelte-5.md).
 */
export class DeviceIssues {
  readonly #client: DeviceIssuesClient;
  #entries = new SvelteMap<string, DeviceIssueEntry>();
  #asked = $state(false);
  #error = $state<Error | undefined>(undefined);
  #flight: Promise<void> | undefined;

  constructor(client: DeviceIssuesClient) {
    this.#client = client;
  }

  /** True once a read has finished, whether it answered or failed. */
  get asked(): boolean {
    return this.#asked;
  }

  /** Why the read failed, when it did. */
  get error(): Error | undefined {
    return this.#error;
  }

  /** What the server says of one reason, or `undefined` for an unknown one. */
  entryOf(reason: string): DeviceIssueEntry | undefined {
    return this.#entries.get(reason);
  }

  /**
   * Reads the vocabulary. A call while one is in flight joins it, and a call
   * after an answer landed does nothing. A failed read is asked again.
   */
  read(): Promise<void> {
    if (this.#asked && this.#error === undefined) return Promise.resolve();

    this.#flight ??= this.#read().finally(() => {
      this.#flight = undefined;
    });

    return this.#flight;
  }

  async #read(): Promise<void> {
    try {
      const answer = await readDeviceIssues(this.#client);
      this.#entries.clear();
      for (const entry of answer.items ?? []) {
        this.#entries.set(entry.reason, entry);
      }
      this.#error = undefined;
    } catch (thrown) {
      this.#entries.clear();
      this.#error =
        thrown instanceof Error ? thrown : new Error(String(thrown));
    } finally {
      this.#asked = true;
    }
  }
}
