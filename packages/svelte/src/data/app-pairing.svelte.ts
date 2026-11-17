import {
  type AppPairing,
  type AppPairingClient,
  approveAppPairing,
  type CredentialScope,
  denyAppPairing,
  readAppPairing,
} from './services/settings';

/** Kept as it was thrown, so a screen can render it by its code. */
const asError = (thrown: unknown): Error =>
  thrown instanceof Error ? thrown : new Error(String(thrown));

/** What a person did with a pairing on this screen. */
export type AppPairingOutcome = 'approved' | 'denied';

/**
 * One pairing an app asked for, read by the code it shows and decided by the
 * signed-in person (ADR-20261025-tpp). A class, because the screen and the app hold
 * the same one (svelte-5.md); the client comes in through the constructor.
 *
 * Nothing here is kept between visits. A pairing waits ten minutes, and a
 * code read on another day names nothing.
 */
export class AppPairingReview {
  readonly #client: AppPairingClient;
  #code = $state('');
  #pairing = $state.raw<AppPairing | undefined>(undefined);
  #loading = $state(false);
  #loaded = $state(false);
  #error = $state<Error | undefined>(undefined);
  #deciding = $state(false);
  #outcome = $state<AppPairingOutcome | undefined>(undefined);
  // Counts the resets and the new codes. An answer to an older one records nothing.
  #turn = 0;

  constructor(client: AppPairingClient) {
    this.#client = client;
  }

  /** The code the person typed or the link carried. */
  get code(): string {
    return this.#code;
  }

  /** The pairing the code names, once read. */
  get pairing(): AppPairing | undefined {
    return this.#pairing;
  }

  get loading(): boolean {
    return this.#loading;
  }

  /** A read has answered, which tells a code nobody typed from one nothing answers. */
  get loaded(): boolean {
    return this.#loaded;
  }

  /** The last refusal, as it was thrown. */
  get error(): Error | undefined {
    return this.#error;
  }

  /** True while an approval or a denial is on its way. */
  get deciding(): boolean {
    return this.#deciding;
  }

  /** What the person decided, once the server took it. */
  get outcome(): AppPairingOutcome | undefined {
    return this.#outcome;
  }

  /**
   * Reads the pairing a code names. The same code read again while it is
   * shown is not read twice.
   */
  async load(code: string): Promise<void> {
    const wanted = code.trim();
    if (wanted === '') return;
    if (wanted === this.#code && (this.#loading || this.#loaded)) return;

    this.#turn += 1;
    const turn = this.#turn;
    this.#code = wanted;
    this.#pairing = undefined;
    this.#outcome = undefined;
    this.#error = undefined;
    this.#loaded = false;
    this.#loading = true;
    try {
      const pairing = await readAppPairing(this.#client, wanted);
      if (turn !== this.#turn) return;
      this.#pairing = pairing;
      this.#loaded = true;
    } catch (thrown) {
      if (turn === this.#turn) {
        this.#error = asError(thrown);
        this.#loaded = true;
      }
    } finally {
      if (turn === this.#turn) this.#loading = false;
    }
  }

  /** Approves the pairing with the scopes named. */
  approve(scopes: readonly CredentialScope[]): Promise<void> {
    if (scopes.length === 0) return Promise.resolve();

    return this.#decide('approved', () =>
      approveAppPairing(this.#client, this.#code, scopes),
    );
  }

  /** Refuses the app its credential. */
  deny(): Promise<void> {
    return this.#decide('denied', () =>
      denyAppPairing(this.#client, this.#code),
    );
  }

  /** Forgets the code and what was read for it. */
  reset(): void {
    this.#turn += 1;
    this.#code = '';
    this.#pairing = undefined;
    this.#loading = false;
    this.#loaded = false;
    this.#error = undefined;
    this.#deciding = false;
    this.#outcome = undefined;
  }

  async #decide(
    outcome: AppPairingOutcome,
    send: () => Promise<void>,
  ): Promise<void> {
    if (this.#pairing === undefined || this.#deciding) return;

    const turn = this.#turn;
    this.#deciding = true;
    this.#error = undefined;
    try {
      await send();
      if (turn !== this.#turn) return;
      this.#outcome = outcome;
    } catch (thrown) {
      if (turn === this.#turn) this.#error = asError(thrown);
    } finally {
      if (turn === this.#turn) this.#deciding = false;
    }
  }
}
