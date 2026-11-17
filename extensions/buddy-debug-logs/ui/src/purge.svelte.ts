// Removing kept records from the page: everything, to start again, or what
// is older than a number of hours. It keeps whether one is running, what the
// last one removed and why one failed.
import type { LogsApi } from './api';

/** The ages a person keeps, in hours. Undefined removes everything. */
export const KEEP_HOURS = [1, 6, 24] as const;
export type KeepHours = (typeof KEEP_HOURS)[number];

/** What one purge removed, over both streams. */
export interface Removed {
  readonly records: number;
  readonly bytes: number;
}

export class PurgeStore {
  readonly #api: Pick<LogsApi, 'purgeRecords'>;
  #busy = $state(false);
  #fault = $state.raw<unknown>(undefined);
  #removed = $state.raw<Removed | undefined>(undefined);

  constructor(api: Pick<LogsApi, 'purgeRecords'>) {
    this.#api = api;
  }

  get busy(): boolean {
    return this.#busy;
  }

  get fault(): unknown {
    return this.#fault;
  }

  /** What the last purge removed, until the next one starts. */
  get removed(): Removed | undefined {
    return this.#removed;
  }

  /**
   * Removes the records older than `keep` hours, or every record when `keep`
   * is undefined. Resolves true once the module removed them.
   */
  async run(keep: KeepHours | undefined): Promise<boolean> {
    if (this.#busy) return false;
    this.#busy = true;
    this.#fault = undefined;
    this.#removed = undefined;
    try {
      const answer = await this.#api.purgeRecords({
        body: keep === undefined ? {} : { older_than_hours: keep },
      });
      const streams = Object.values(answer.purged);
      this.#removed = {
        records: streams.reduce((sum, one) => sum + one.records, 0),
        bytes: streams.reduce((sum, one) => sum + one.bytes, 0),
      };

      return true;
    } catch (thrown) {
      this.#fault = thrown;

      return false;
    } finally {
      this.#busy = false;
    }
  }
}
