// The folders this server shares with the Fractal servers below it, read and
// switched through the Fractal module's own routes (ADR-20261023-7r3 rules
// 12 to 15). Storage draws them as its Shared column.
import { ApiError } from '@prusa/connect';
import type { components, Operations } from 'fractal-client';

export type Share = components['schemas']['Share'];

/** What this store needs of the module's client, so a test hands in a fake (svelte-data.md). */
export type FractalSharesClient = Pick<
  Operations,
  'listFractalShares' | 'shareFractalFolder' | 'unshareFractalFolder'
>;

// The module answers 404 on a server whose link has no files block.
const isOff = (thrown: unknown): boolean =>
  thrown instanceof ApiError && thrown.status === 404;

const holds = (share: Share, storage: string, path: string): boolean =>
  share.storage === storage &&
  (path === share.path || path.startsWith(`${share.path}/`));

export class FractalShares {
  readonly #client: FractalSharesClient;
  // The server owns the whole list, so it is replaced whole on every read
  // (svelte-5.md).
  #items = $state.raw<readonly Share[]>([]);
  // Undefined until a read answered: then true where the server shares files
  // and false where its link runs without them.
  #on = $state<boolean | undefined>(undefined);
  #error = $state<unknown>(undefined);
  // One flight per piece of data (svelte-data.md).
  #flight: Promise<void> | undefined;
  // Counts the reads and the resets. Only the answer to the latest read is
  // kept, so an older read that lands later cannot undo a share.
  #generation = 0;

  constructor(client: FractalSharesClient) {
    this.#client = client;
  }

  /** Every shared folder, sorted by storage and path as the module sends them. */
  get items(): readonly Share[] {
    return this.#items;
  }

  /** Whether this server shares files at all, and undefined before a read answered. */
  get on(): boolean | undefined {
    return this.#on;
  }

  /** The last refusal of a read, as it was thrown. A server without files is not one. */
  get error(): unknown {
    return this.#error;
  }

  // ponytail: no route says whether this person may change a share, so the
  // switch is offered and a refusal is shown when it comes. Read the grant
  // when the module answers it.
  readonly canChange = true;

  load(): Promise<void> {
    this.#flight ??= this.#read().finally(() => {
      this.#flight = undefined;
    });

    return this.#flight;
  }

  /**
   * The shared folder that holds `path`: the folder itself when it is shared,
   * else the outermost shared folder above it, else undefined.
   */
  sharedRoot(storage: string, path: string): string | undefined {
    if (this.#items.some((one) => one.storage === storage && one.path === path))
      return path;

    return this.#items.find((one) => holds(one, storage, path))?.path;
  }

  /** Shares a folder or stops sharing it, then reads the list again. Throws the refusal. */
  async share(storage: string, path: string, shared: boolean): Promise<void> {
    const request = { path: { storage, path } };
    if (shared) {
      await this.#client.shareFractalFolder(request);
    } else {
      await this.#client.unshareFractalFolder(request);
    }
    await this.#read();
  }

  reset(): void {
    this.#generation++;
    this.#items = [];
    this.#on = undefined;
    this.#error = undefined;
    this.#flight = undefined;
  }

  async #read(): Promise<void> {
    const generation = ++this.#generation;
    try {
      const answer = await this.#client.listFractalShares();
      if (generation !== this.#generation) return;
      this.#items = answer.items;
      this.#on = true;
      this.#error = undefined;
    } catch (thrown) {
      if (generation !== this.#generation) return;
      if (isOff(thrown)) {
        this.#items = [];
        this.#on = false;
        this.#error = undefined;
      } else {
        // The last list read stays on screen with the fault beside it.
        this.#error = thrown;
      }
    }
  }
}
