// A download a person starts from the page: one stream, every page of it,
// saved as one file. It keeps whether one is running and why one failed.
import type { LogsApi } from './api';
import { exportStream, fileName, type Stream } from './export';

/** Saves a file the browser holds. Handed in, so a test reads what was saved. */
export type Save = (blob: Blob, name: string) => void;

export class DownloadStore {
  readonly #api: Pick<LogsApi, 'exportRecords'>;
  readonly #save: Save;
  #busy = $state(false);
  #fault = $state.raw<unknown>(undefined);

  constructor(api: Pick<LogsApi, 'exportRecords'>, save: Save) {
    this.#api = api;
    this.#save = save;
  }

  get busy(): boolean {
    return this.#busy;
  }

  get fault(): unknown {
    return this.#fault;
  }

  /** Fetches every page of a stream for the window and saves it. */
  async run(stream: Stream, since: string, device?: string): Promise<void> {
    if (this.#busy) return;
    this.#busy = true;
    this.#fault = undefined;
    try {
      const blob = await exportStream(this.#api, {
        stream,
        since,
        ...(device === undefined ? {} : { device }),
      });
      this.#save(blob, fileName(stream, Date.now()));
    } catch (thrown) {
      this.#fault = thrown;
    } finally {
      this.#busy = false;
    }
  }
}

/** The browser's own way to save a file: a link to it, clicked once. */
export const saveInBrowser: Save = (blob, name) => {
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = name;
  link.click();
  setTimeout(() => {
    URL.revokeObjectURL(url);
  }, 0);
};
