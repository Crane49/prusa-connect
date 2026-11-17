import {
  type RestartMode,
  restartServer,
  type ServerRestartClient,
} from './services/maintenance';

export type ServerRestartPhase =
  'idle' | 'sending' | 'waiting' | 'timeout' | 'failed';

/** Why the server refused the request, for the two refusals a screen words on their own. */
export type ServerRestartRefusal = 'forbidden' | 'upgrade-busy';

export interface ServerRestartOptions {
  /** How long the page waits for the server to come back. */
  deadlineMs?: number;
  /** How long between two reads while it waits. */
  intervalMs?: number;
  /** Waits; a test hands in one that does not. */
  sleep?: (ms: number) => Promise<void>;
  /** The clock; a test hands in its own. */
  now?: () => number;
  /** Loads the page again once the server is in the state asked for. */
  reload?: () => void;
}

const DEADLINE_MS = 90_000;
const INTERVAL_MS = 1000;
// A connection that hangs while the server shuts down must not outlast a poll.
const READ_TIMEOUT_MS = 5000;

const pause = (ms: number): Promise<void> =>
  new Promise((resolve) => {
    setTimeout(resolve, ms);
  });

const statusOf = (thrown: unknown): unknown =>
  typeof thrown === 'object' && thrown !== null
    ? (thrown as { status?: unknown }).status
    : undefined;

/**
 * A restart the operator asked for, and the wait for the server to come back
 * in the state asked for. The restart drops the connection, so the wait reads
 * `GET /api/v1/ui` until it answers with `safe_mode` present (safe) or absent
 * (normal), then reloads the page.
 *
 * A restart into the state the server is already in is refused before it is
 * sent, because the old server would pass for the new one. Whether the person
 * may restart is not asked here: the Server room's maintenance read answers
 * 403 for anyone else.
 */
export class ServerRestart {
  readonly #client: ServerRestartClient;
  readonly #deadline: number;
  readonly #interval: number;
  readonly #sleep: (ms: number) => Promise<void>;
  readonly #now: () => number;
  readonly #reload: () => void;
  #phase = $state<ServerRestartPhase>('idle');
  #error = $state<unknown>(undefined);
  #run = 0;

  constructor(client: ServerRestartClient, options: ServerRestartOptions = {}) {
    this.#client = client;
    this.#deadline = options.deadlineMs ?? DEADLINE_MS;
    this.#interval = options.intervalMs ?? INTERVAL_MS;
    this.#sleep = options.sleep ?? pause;
    this.#now = options.now ?? Date.now;
    this.#reload =
      options.reload ??
      ((): void => {
        location.reload();
      });
  }

  get phase(): ServerRestartPhase {
    return this.#phase;
  }

  /** What the refused request threw. Set while the phase is `failed`. */
  get error(): unknown {
    return this.#error;
  }

  /** The refusal a screen words on its own: 403 not an organisation admin, 409 an upgrade is running. */
  get refusal(): ServerRestartRefusal | undefined {
    if (this.#phase !== 'failed') return undefined;
    const status = statusOf(this.#error);
    if (status === 403) return 'forbidden';

    return status === 409 ? 'upgrade-busy' : undefined;
  }

  /** Stops the wait for good: nothing reloads after it. Call it when the screen goes away. */
  cancel(): void {
    this.#run += 1;
    this.#phase = 'idle';
  }

  /**
   * Sends the request and waits for the server to be back in the state `mode`
   * names. Does nothing, and sends nothing, when the server is already in that
   * state (`safe` in safe mode, `normal` outside it).
   */
  async restart(mode: RestartMode): Promise<void> {
    if (this.#phase === 'sending' || this.#phase === 'waiting') return;
    this.#run += 1;
    const run = this.#run;
    this.#phase = 'sending';
    this.#error = undefined;
    if (await this.#isIn(mode)) {
      if (run === this.#run) this.#phase = 'idle';

      return;
    }
    if (run !== this.#run) return;
    try {
      await restartServer(this.#client, mode);
    } catch (thrown) {
      if (run !== this.#run) return;
      this.#error = thrown;
      this.#phase = 'failed';

      return;
    }
    if (run !== this.#run) return;

    await this.#waitFor(mode, run);
  }

  async #waitFor(mode: RestartMode, run: number): Promise<void> {
    this.#phase = 'waiting';
    const started = this.#now();

    while (run === this.#run) {
      await this.#sleep(this.#interval);
      if (run !== this.#run) return;
      const inMode = await this.#isIn(mode);
      if (run !== this.#run) return;
      if (inMode) {
        this.#reload();

        return;
      }
      if (this.#now() - started >= this.#deadline) {
        this.#phase = 'timeout';

        return;
      }
    }
  }

  /** Whether the server answers in the state `mode` names. A failed read is the server not being there, so false. */
  async #isIn(mode: RestartMode): Promise<boolean> {
    try {
      const settings = await this.#client.getUiSettings(
        {},
        AbortSignal.timeout(READ_TIMEOUT_MS),
      );

      return (mode === 'safe') === (settings.safe_mode !== undefined);
    } catch {
      return false;
    }
  }
}
