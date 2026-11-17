// Moved from frontend/src/lib/stores/session.svelte.ts, 2026-09-11
import type { ModulePrincipal } from '../domain/principal';
import {
  type AuthClient,
  changeOwnPassword,
  recordSetup,
  readRedirectProvider,
  readSetup,
  type SetupDecision,
  type SetupState,
  signIn,
  signOut,
  whoami,
} from './services/auth';

/**
 * Whether this browser holds a session. `unknown` is the state before the
 * first answer, and it is told from `signed-out` on purpose: a screen that
 * drew the sign-in while the question was still open would flash it at
 * somebody who is signed in (svelte-data.md).
 */
export type SessionStatus = 'unknown' | 'signed-out' | 'signed-in';

/** Why the sign-in is showing, which decides the sentence above the form. */
export type SessionEnd = 'never' | 'refused' | 'asked';

/**
 * The fault as it was thrown, and never as a sentence. A screen renders it
 * through `describeFault`, which reads the registry code and keeps the
 * server's English message beside the translation (frontend-i18n.md rule 5).
 * Flattening it to a message here would throw the code away.
 */
const asError = (thrown: unknown): Error =>
  thrown instanceof Error ? thrown : new Error(String(thrown));

/**
 * Whether a throw is an answer from the server rather than a browser that
 * could not reach it. The SDK's `ApiError` carries the status, and this reads
 * the shape rather than the class: this package imports the SDK's types and
 * never its runtime, so a page holds one copy of the SDK (ADR-20260911-4p6 rule 27).
 */
const isRefusal = (thrown: unknown): boolean =>
  // The narrowing cast reads one field off a value the SDK threw, which is a
  // trust boundary (typescript.md).
  thrown instanceof Error &&
  typeof (thrown as { status?: unknown }).status === 'number';

/**
 * A shared store, so a class (svelte-5.md). The client comes in through the
 * constructor, which is what lets a test hand it one built on a fake and what
 * lets the host hand it its own.
 */
export class Session {
  readonly #client: AuthClient;
  #status = $state<SessionStatus>('unknown');
  #ended = $state<SessionEnd>('never');
  #principal = $state.raw<ModulePrincipal | undefined>(undefined);
  #error = $state<Error | undefined>(undefined);
  #busy = $state(false);
  // The first run. It is here and not in a store of its own because it is the
  // same question the session asks: what this server does about identity, and
  // who this browser is once it has been answered.
  #setup = $state.raw<SetupState | undefined>(undefined);
  #setupAsked = $state(false);
  #setupError = $state<Error | undefined>(undefined);
  #setupFlight: Promise<void> | undefined;
  // The provider a sign-in is sent to, `''` while the server takes a password,
  // and `undefined` until `get-auth-info` has answered.
  #redirect = $state<string | undefined>(undefined);
  #redirectFlight: Promise<string> | undefined;
  #changing = $state(false);
  #changed = $state(false);
  #passwordFault = $state<unknown>(undefined);

  constructor(client: AuthClient) {
    this.#client = client;
  }

  /** Read-only by design: only the three methods below move it. */
  get status(): SessionStatus {
    return this.#status;
  }

  /** Why the sign-in is showing. Read-only by design. */
  get ended(): SessionEnd {
    return this.#ended;
  }

  /** The signed-in person and what they hold, or `undefined` while nobody is. */
  get principal(): ModulePrincipal | undefined {
    return this.#principal;
  }

  /**
   * The identity provider the sign-in hands over to, or `undefined` when this
   * server takes a username and a password. Known by the time `status` is
   * `signed-out`, and shortly after it is `signed-in`. Read-only by design.
   */
  get redirectProvider(): string | undefined {
    return this.#redirect === '' ? undefined : this.#redirect;
  }

  /** The last refusal, in the server's own words. Cleared by the next attempt. */
  get error(): Error | undefined {
    return this.#error;
  }

  /** True while a request of this store's is in flight. */
  get busy(): boolean {
    return this.#busy;
  }

  /**
   * What this server says about its first run: whether a decision is
   * recorded, the build, and the modules a decision may switch. `undefined`
   * until `readSetup` answers. Read-only by design.
   */
  get setupState(): SetupState | undefined {
    return this.#setup;
  }

  /**
   * True once a first-run read has finished, whether it answered or failed.
   * The page waits on this rather than on the state, so a server that cannot
   * answer draws its refusal instead of an empty screen.
   */
  get setupAsked(): boolean {
    return this.#setupAsked;
  }

  /** Why the first-run read failed, when it did. */
  get setupError(): Error | undefined {
    return this.#setupError;
  }

  /**
   * Asks this server about its first run. A second call while the first is in
   * flight joins it rather than making a second request (svelte-data.md).
   */
  readSetup(): Promise<void> {
    // How a person signs in is a fact of the first run. The wizard restarts
    // the server on a provider, so the answer the page holds from before the
    // restart is the old process's, and the next sign-in screen asks again.
    this.#redirectFlight = undefined;
    this.#setupFlight ??= this.#readSetup().finally(() => {
      this.#setupFlight = undefined;
    });

    return this.#setupFlight;
  }

  async #readSetup(): Promise<void> {
    try {
      this.#setup = await readSetup(this.#client);
      this.#setupError = undefined;
    } catch (thrown) {
      this.#setup = undefined;
      this.#setupError = asError(thrown);
    } finally {
      this.#setupAsked = true;
    }
  }

  /**
   * Records what this server does about identity. It answers true when the
   * decision was taken, and false when it was refused: the screen keeps what
   * was typed and draws the refusal, so the one field the server named is the
   * only one to change.
   *
   * A decision that created an administrator carries their session cookie, so
   * the principal is read again here and the page continues as them.
   */
  async setup(decision: SetupDecision): Promise<boolean> {
    this.#busy = true;
    this.#error = undefined;
    try {
      const answer = await recordSetup(this.#client, decision);
      this.#setup =
        this.#setup === undefined
          ? undefined
          : {
              ...this.#setup,
              mode: answer.mode,
              // A server that restarts records the decision in its next
              // process, so until then this one is still pending.
              state: answer.state === 'done' ? 'done' : this.#setup.state,
            };
      this.#busy = false;
      // The principal is read again whatever the answer said. A required
      // login that created the administrator answers signed_in, and an open
      // server answers it false while serving every request as the configured
      // principal, so the read is what moves the store off signed-out.
      await this.read();

      return true;
    } catch (thrown) {
      this.#error = asError(thrown);
      this.#busy = false;

      return false;
    }
  }

  /**
   * Asks the core who this browser is. A refusal is the answer "nobody", not
   * a failure to show: it is the only way a page with an HttpOnly cookie can
   * learn whether the cookie is still accepted.
   */
  async read(): Promise<void> {
    this.#busy = true;
    try {
      const principal = await whoami(this.#client);
      this.#principal = principal;
      this.#status = 'signed-in';
      this.#error = undefined;
      // Asked now, so the account screen knows whether the person has a
      // password here, and a sign-out later has the answer and does not keep
      // the fleet on screen while it waits for the network.
      void this.#askRedirect().then((provider) => {
        this.#redirect = provider;
      });
    } catch (thrown) {
      // A refusal counts as "your credential stopped working" only when this
      // page had a session a moment ago. On a first visit the browser holds
      // no cookie at all, and the same 401 comes back: telling that person
      // their credential was rejected is not true, so the opening stays the
      // one for somebody who has never signed in.
      const wasSignedIn = this.#status === 'signed-in';
      if (wasSignedIn && this.#ended === 'never' && isRefusal(thrown)) {
        this.#ended = 'refused';
      }
      await this.#leave();
    } finally {
      this.#busy = false;
    }
  }

  /** True while a password change is in flight. Read-only by design. */
  get changing(): boolean {
    return this.#changing;
  }

  /**
   * True once a password change was accepted. It goes false again when the
   * next attempt starts, which is what lets the account screen empty its
   * fields exactly once.
   */
  get changed(): boolean {
    return this.#changed;
  }

  /** What a password change was refused with, as it was thrown. */
  get passwordFault(): unknown {
    return this.#passwordFault;
  }

  /**
   * Changes the signed-in person's own password. The server takes the current
   * one for an account changing its own, so both go up together.
   *
   * The account is read off the principal: the graph keys a person as
   * `user:alice` and the password route takes the part after the colon. A
   * browser with no principal has nobody to change a password for.
   */
  async changePassword(current: string, next: string): Promise<void> {
    const subject = this.#principal?.id;
    if (subject === undefined) return;

    this.#changing = true;
    this.#changed = false;
    this.#passwordFault = undefined;
    try {
      await changeOwnPassword(
        this.#client,
        subject.slice(subject.indexOf(':') + 1),
        current,
        next,
      );
      this.#changed = true;
    } catch (thrown) {
      this.#passwordFault = thrown;
    } finally {
      this.#changing = false;
    }
  }

  /** Signs in, then reads who that turned out to be. */
  async signIn(username: string, password: string): Promise<void> {
    this.#busy = true;
    this.#error = undefined;
    try {
      await signIn(this.#client, username, password);
    } catch (thrown) {
      this.#error = asError(thrown);
      this.#busy = false;
      return;
    }
    this.#busy = false;
    this.#ended = 'never';
    await this.read();
  }

  /**
   * Ends the session. The screen returns to the sign-in whether or not the
   * core answered: the browser is not to keep showing a fleet somebody asked
   * to leave.
   */
  async signOut(): Promise<void> {
    this.#busy = true;
    try {
      await signOut(this.#client);
      this.#error = undefined;
    } catch (thrown) {
      this.#error = asError(thrown);
    } finally {
      this.#ended = 'asked';
      await this.#leave();
      this.#busy = false;
    }
  }

  /** How this server signs a person in, asked once per page. */
  #askRedirect(): Promise<string> {
    this.#redirectFlight ??= readRedirectProvider(this.#client).then(
      (provider) => provider ?? '',
    );
    return this.#redirectFlight;
  }

  /**
   * Moves the page to signed out. The provider is known before the status
   * moves, so the sign-in is never drawn as a password form and then swapped
   * for the redirect.
   */
  async #leave(): Promise<void> {
    this.#principal = undefined;
    this.#redirect = await this.#askRedirect();
    this.#status = 'signed-out';
  }
}
