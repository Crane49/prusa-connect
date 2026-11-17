// The first-run wizard's conversation with the server: the configuration it
// asks for, the confirm, and the wait while the server restarts (ADR-20261015-ejz).
import {
  checkSetupOidc,
  checkSetupStorage,
  createSetupCertificates,
  probeHealth,
  readSetup,
  readSetupConfiguration,
  recordSetup,
  type SetupCertificates,
  type SetupConfiguration,
  type SetupDecision,
  type SetupFailure,
  type SetupOidcCheck,
  type SetupOidcProbe,
  type SetupStorageCheck,
  type SetupWizardClient,
} from './services/auth';

/**
 * Where the confirm stands. `editing` is before it, `sending` while the call
 * is in flight, `waiting` while the server restarts, `moved` when it restarts
 * on an address this page cannot reach, `ready` once the decision is
 * recorded, `failed` when the server came back on its previous configuration
 * with an error, and `timeout` when it never came back.
 */
export type SetupPhase =
  'editing' | 'sending' | 'waiting' | 'moved' | 'ready' | 'failed' | 'timeout';

export interface SetupFlowOptions {
  /** How long the page waits for the server to come back. */
  deadlineMs?: number;
  /** How long between two reads while it waits. */
  intervalMs?: number;
  /** Waits; a test hands in one that does not. */
  sleep?: (ms: number) => Promise<void>;
  /** The clock; a test hands in its own. */
  now?: () => number;
}

const DEADLINE_MS = 90_000;
const INTERVAL_MS = 1000;

const pause = (ms: number): Promise<void> =>
  new Promise((resolve) => {
    setTimeout(resolve, ms);
  });

const asError = (thrown: unknown): Error =>
  thrown instanceof Error ? thrown : new Error(String(thrown));

/**
 * A shared store, so a class (svelte-5.md). The draft the person types is not
 * here: it is client state and the wizard's own (svelte-data.md).
 */
export class SetupFlow {
  readonly #client: SetupWizardClient;
  readonly #deadline: number;
  readonly #interval: number;
  readonly #sleep: (ms: number) => Promise<void>;
  readonly #now: () => number;
  #contract = $state.raw<SetupConfiguration | undefined>(undefined);
  #reading = $state(false);
  #readError = $state<Error | undefined>(undefined);
  #phase = $state<SetupPhase>('editing');
  #error = $state<Error | undefined>(undefined);
  #failure = $state.raw<SetupFailure | undefined>(undefined);
  #elapsed = $state(0);
  #storage = $state.raw<SetupStorageCheck | undefined>(undefined);
  #storageError = $state<Error | undefined>(undefined);
  #checking = $state(false);
  #oidc = $state.raw<SetupOidcCheck | undefined>(undefined);
  #oidcError = $state<Error | undefined>(undefined);
  #trying = $state(false);
  #certificates = $state.raw<SetupCertificates | undefined>(undefined);
  #certificatesError = $state<Error | undefined>(undefined);
  #making = $state(false);
  #read: AbortController | undefined;
  #waitFor = 0;
  #earlierFailure: string | undefined;

  constructor(client: SetupWizardClient, options: SetupFlowOptions = {}) {
    this.#client = client;
    this.#deadline = options.deadlineMs ?? DEADLINE_MS;
    this.#interval = options.intervalMs ?? INTERVAL_MS;
    this.#sleep = options.sleep ?? pause;
    this.#now = options.now ?? Date.now;
  }

  /** The keys the wizard may set, once the code has been accepted. */
  get contract(): SetupConfiguration | undefined {
    return this.#contract;
  }

  /** True while the configuration is being read. */
  get reading(): boolean {
    return this.#reading;
  }

  /** Why the configuration could not be read: a wrong code, most often. */
  get readError(): Error | undefined {
    return this.#readError;
  }

  /** Where the confirm stands. Read-only by design. */
  get phase(): SetupPhase {
    return this.#phase;
  }

  /** Why the confirm was refused. The answers stay as they were typed. */
  get error(): Error | undefined {
    return this.#error;
  }

  /** The start that failed on the confirmed configuration. */
  get failure(): SetupFailure | undefined {
    return this.#failure;
  }

  /** The last answer about the folder for uploads. */
  get storage(): SetupStorageCheck | undefined {
    return this.#storage;
  }

  /** Why the folder could not be checked at all. */
  get storageError(): Error | undefined {
    return this.#storageError;
  }

  /** True while a folder is being checked. */
  get checking(): boolean {
    return this.#checking;
  }

  /**
   * Asks the server whether it could keep uploads in a folder, and answers
   * whether it could. The answer stays for the screen to explain.
   */
  async checkStorage(code: string, root: string): Promise<boolean> {
    this.#checking = true;
    this.#storageError = undefined;
    try {
      this.#storage = await checkSetupStorage(this.#client, code, root);

      return this.#storage.writable;
    } catch (thrown) {
      this.#storage = undefined;
      this.#storageError = asError(thrown);

      return false;
    } finally {
      this.#checking = false;
    }
  }

  /** What the last try of the sign-in provider found. */
  get oidc(): SetupOidcCheck | undefined {
    return this.#oidc;
  }

  /** Why the sign-in provider could not be tried at all. */
  get oidcError(): Error | undefined {
    return this.#oidcError;
  }

  /** True while the sign-in provider is being tried. */
  get trying(): boolean {
    return this.#trying;
  }

  /** Forgets the last try, once the answers it was for have changed. */
  forgetOidc(): void {
    this.#oidc = undefined;
    this.#oidcError = undefined;
  }

  /**
   * Asks the server to try a sign-in provider, and keeps what it found for
   * the screen. Signs nobody in.
   */
  async checkOidc(code: string, probe: SetupOidcProbe): Promise<void> {
    this.#trying = true;
    this.#oidcError = undefined;
    try {
      this.#oidc = await checkSetupOidc(this.#client, code, probe);
    } catch (thrown) {
      this.#oidc = undefined;
      this.#oidcError = asError(thrown);
    } finally {
      this.#trying = false;
    }
  }

  /** The certificates the server made in this visit. */
  get certificates(): SetupCertificates | undefined {
    return this.#certificates;
  }

  /** Why the certificates could not be made. */
  get certificatesError(): Error | undefined {
    return this.#certificatesError;
  }

  /** True while the server makes them. */
  get making(): boolean {
    return this.#making;
  }

  /** Asks the server to make the certificates for the names given. */
  async createCertificates(code: string, names: string[]): Promise<boolean> {
    this.#making = true;
    this.#certificatesError = undefined;
    try {
      this.#certificates = await createSetupCertificates(
        this.#client,
        code,
        names,
      );

      return true;
    } catch (thrown) {
      this.#certificatesError = asError(thrown);

      return false;
    } finally {
      this.#making = false;
    }
  }

  /** How far the wait has gone, from 0 to 1. */
  get elapsed(): number {
    return this.#elapsed;
  }

  /**
   * Reads the configuration with the setup code. It answers false when the
   * code was refused, and the screen draws `readError`. A read that a newer
   * one replaced is aborted (svelte-data.md).
   */
  async readContract(code: string): Promise<boolean> {
    this.#read?.abort();
    const controller = new AbortController();
    this.#read = controller;
    this.#reading = true;
    this.#readError = undefined;
    try {
      const contract = await readSetupConfiguration(
        this.#client,
        code,
        controller.signal,
      );
      if (controller.signal.aborted) return false;
      this.#contract = contract;

      return true;
    } catch (thrown) {
      if (controller.signal.aborted) return false;
      this.#readError = asError(thrown);

      return false;
    } finally {
      if (this.#read === controller) {
        this.#reading = false;
        this.#read = undefined;
      }
    }
  }

  /**
   * Sends the confirmed wizard. A server that restarts is waited on here when
   * it comes back on this page's address. When it moves, `here` is false and
   * the phase is `moved`: the page opens the new address itself.
   */
  async confirm(decision: SetupDecision, here = true): Promise<void> {
    this.#phase = 'sending';
    try {
      this.#earlierFailure = (await readSetup(this.#client)).last_error?.at;
    } catch {
      this.#earlierFailure = undefined;
    }
    this.#error = undefined;
    this.#failure = undefined;
    try {
      const answer = await recordSetup(this.#client, decision);
      if (!answer.restarting) {
        this.#phase = 'ready';

        return;
      }
    } catch (thrown) {
      this.#error = asError(thrown);
      this.#phase = 'editing';

      return;
    }

    if (!here) {
      this.#phase = 'moved';

      return;
    }

    await this.#waitForRestart();
  }

  /** Waits again after a wait that ran out. */
  async keepWaiting(): Promise<void> {
    await this.#waitForRestart();
  }

  /** Back to the answers, after a failure or a wait that ran out. */
  edit(): void {
    this.#waitFor += 1;
    this.#phase = 'editing';
  }

  async #waitForRestart(): Promise<void> {
    this.#waitFor += 1;
    const run = this.#waitFor;
    this.#phase = 'waiting';
    this.#elapsed = 0;
    const started = this.#now();

    while (run === this.#waitFor) {
      await this.#sleep(this.#interval);
      if (run !== this.#waitFor) return;

      const spent = this.#now() - started;
      this.#elapsed = Math.min(1, spent / this.#deadline);
      if (await this.#settled()) return;

      if (spent >= this.#deadline) {
        this.#phase = 'timeout';

        return;
      }
    }
  }

  /**
   * One read of the restarting server. Until it answers, every refusal is the
   * server not being there yet, and the wait goes on.
   */
  async #settled(): Promise<boolean> {
    try {
      await probeHealth(this.#client);
      const state = await readSetup(this.#client);
      // A failure from before this confirm is not news. The server keeps the
      // last one, and its time is what tells two apart.
      const fresh =
        state.last_error !== undefined &&
        state.last_error.at !== this.#earlierFailure;
      if (state.state === 'done') {
        // Done with a fresh failure is a start that recorded the decision and
        // then went back to the configuration it had: the page says so.
        this.#failure = fresh ? state.last_error : undefined;
        this.#phase = 'ready';

        return true;
      }
      if (fresh) {
        this.#failure = state.last_error;
        this.#phase = 'failed';

        return true;
      }
    } catch {
      // Not back yet.
    }

    return false;
  }
}
