// What a service module's instance did lately: its memory over the last hour
// and what it wrote to standard error, read through the SDK. A reading that
// failed leaves the last good one on screen.
import { ApiError, type components, type PrusaConnect } from '@prusa/connect';

import type { ResourceInstance } from '@prusa/connect-ui/data';

export type ResourceMemoryHistory =
  components['schemas']['ResourceMemoryHistory'];
export type ResourceLog = components['schemas']['ResourceLog'];

/** What this store needs of the SDK, so a test hands in a fake (svelte-data.md). */
export type ModuleOperationsClient = Pick<
  PrusaConnect,
  'getResourceMemory' | 'getResourceLog'
>;

type ResourceKind = Parameters<
  PrusaConnect['getResourceLog']
>[0]['path']['kind'];

const keyOf = (instance: ResourceInstance): string =>
  `${instance.kind}:${instance.name}`;

const isNotFound = (thrown: unknown): boolean =>
  thrown instanceof ApiError && thrown.status === 404;

export class ModuleOperations {
  readonly #client: ModuleOperationsClient;

  #histories = $state.raw<Readonly<Record<string, ResourceMemoryHistory>>>({});
  #logs = $state.raw<Readonly<Record<string, ResourceLog>>>({});
  #refused = $state.raw<Readonly<Record<string, boolean>>>({});
  #missing = $state.raw<Readonly<Record<string, boolean>>>({});
  #historyFaults = $state.raw<Readonly<Record<string, unknown>>>({});
  #logFaults = $state.raw<Readonly<Record<string, unknown>>>({});
  // The reads in flight, so two callers share one request. No page draws it,
  // so it is not reactive.
  // eslint-disable-next-line svelte/prefer-svelte-reactivity
  readonly #flights = new Map<string, Promise<void>>();
  #resets = 0;

  constructor(client: ModuleOperationsClient) {
    this.#client = client;
  }

  /** The memory readings and failed stops of the last hour; undefined until read. */
  history(instance: ResourceInstance): ResourceMemoryHistory | undefined {
    return this.#histories[keyOf(instance)];
  }

  /** The newest log lines; undefined until read, or when the person may not read them. */
  log(instance: ResourceInstance): ResourceLog | undefined {
    return this.#logs[keyOf(instance)];
  }

  /** The server refused the log (403): the person may not read it, which is no failure. */
  logRefused(instance: ResourceInstance): boolean {
    return this.#refused[keyOf(instance)] === true;
  }

  /** The server answered 404 for the instance: it is undeclared or no longer visible. */
  missing(instance: ResourceInstance): boolean {
    return this.#missing[keyOf(instance)] === true;
  }

  /** What the last history read failed with, other than a missing instance; undefined after a good read. */
  historyFault(instance: ResourceInstance): unknown {
    return this.#historyFaults[keyOf(instance)];
  }

  /** What the last log read failed with, other than a refusal or a missing instance. */
  logFault(instance: ResourceInstance): unknown {
    return this.#logFaults[keyOf(instance)];
  }

  /**
   * Reads the history and the log of one instance, or only one of them; a
   * read already in flight is the one returned.
   */
  load(instance: ResourceInstance, only?: 'history' | 'log'): Promise<void> {
    const key = `${keyOf(instance)}:${only ?? 'both'}`;
    const running = this.#flights.get(key);
    if (running !== undefined) return running;

    const flight: Promise<void> = Promise.all([
      only === 'log' ? undefined : this.#readHistory(instance),
      only === 'history' ? undefined : this.#readLog(instance),
    ])
      .then(() => undefined)
      .finally(() => {
        // A reset may have replaced this flight with a newer one.
        if (this.#flights.get(key) === flight) this.#flights.delete(key);
      });
    this.#flights.set(key, flight);

    return flight;
  }

  /** Forgets what was read for the person who left. */
  reset(): void {
    this.#resets += 1;
    this.#flights.clear();
    this.#histories = {};
    this.#logs = {};
    this.#refused = {};
    this.#missing = {};
    this.#historyFaults = {};
    this.#logFaults = {};
  }

  async #readHistory(instance: ResourceInstance): Promise<void> {
    const resets = this.#resets;
    const key = keyOf(instance);
    try {
      const history = await this.#client.getResourceMemory({
        path: { kind: instance.kind as ResourceKind, name: instance.name },
      });
      if (resets !== this.#resets) return;
      this.#histories = { ...this.#histories, [key]: history };
      this.#missing = { ...this.#missing, [key]: false };
      this.#historyFaults = { ...this.#historyFaults, [key]: undefined };
    } catch (thrown) {
      if (resets !== this.#resets) return;
      // A failure leaves the last reading, and is kept beside it.
      if (isNotFound(thrown)) this.#missing = { ...this.#missing, [key]: true };
      else this.#historyFaults = { ...this.#historyFaults, [key]: thrown };
    }
  }

  async #readLog(instance: ResourceInstance): Promise<void> {
    const resets = this.#resets;
    const key = keyOf(instance);
    try {
      const log = await this.#client.getResourceLog({
        path: { kind: instance.kind as ResourceKind, name: instance.name },
      });
      if (resets !== this.#resets) return;
      this.#logs = { ...this.#logs, [key]: log };
      this.#refused = { ...this.#refused, [key]: false };
      this.#logFaults = { ...this.#logFaults, [key]: undefined };
    } catch (thrown) {
      if (resets !== this.#resets) return;
      // Only a 403 is a refusal; a failure after one says nothing about it.
      this.#refused = {
        ...this.#refused,
        [key]: thrown instanceof ApiError && thrown.status === 403,
      };
      if (isNotFound(thrown)) this.#missing = { ...this.#missing, [key]: true };
      else if (!(thrown instanceof ApiError && thrown.status === 403))
        this.#logFaults = { ...this.#logFaults, [key]: thrown };
    }
  }
}
