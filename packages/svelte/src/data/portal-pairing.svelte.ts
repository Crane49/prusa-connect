import type { ErrorCode } from '@prusa/connect';

import { type DataFault, faultOf, NETWORK_FAULT } from './fault';
import type {
  PortalPairingStatus,
  UpgradeDeliveryService,
} from './services/maintenance';

/** The four pairing calls. The upgrade delivery service carries them. */
export type PortalPairingService = Pick<
  UpgradeDeliveryService,
  | 'getPortalPairingStatus'
  | 'startPortalPairing'
  | 'refreshPortalPairing'
  | 'disconnectPortal'
>;

/** What the person pressed, while its answer is still on the way. */
export type PortalPairingAction = 'start' | 'disconnect' | 'retry';

/**
 * The screen the pairing wizard draws. It is read from the server's state,
 * the last refusal and whether the person has opened the portal yet.
 */
export type PortalPairingPhase =
  | 'loading'
  | 'intro'
  | 'code'
  | 'confirm'
  | 'approval'
  | 'paired'
  | 'expired'
  | 'denied'
  | 'revoked'
  | 'clock'
  | 'unreachable';

export const CLOCK_SKEW_FAULT =
  'core.resource/BETA_PORTAL_CLOCK_SKEW' satisfies ErrorCode;
export const PORTAL_UNAVAILABLE_FAULT =
  'core.resource/BETA_PORTAL_UNAVAILABLE' satisfies ErrorCode;
export const ALREADY_PAIRED_FAULT =
  'core.resource/BETA_PORTAL_ALREADY_PAIRED' satisfies ErrorCode;
export const REVOCATION_INCOMPLETE_FAULT =
  'core.resource/BETA_PORTAL_REVOCATION_INCOMPLETE' satisfies ErrorCode;

/** Used when the server names no interval: RFC 8628 says five seconds. */
export const DEFAULT_POLL_MS = 5000;
/** A portal that keeps failing is asked at most once a minute. */
export const MAX_POLL_MS = 60_000;
/**
 * How often an open page asks a paired server to check its pairing with the
 * portal. The server checks at most this often whatever the page asks.
 */
export const VERIFY_MS = 60_000;

export interface PortalPairingOptions {
  /** Called when a pending pairing on this page becomes paired. */
  readonly onPaired?: () => void;
  /** Timers, replaceable in a test. */
  readonly setTimer?: (run: () => void, ms: number) => unknown;
  readonly clearTimer?: (timer: unknown) => void;
}

/**
 * The Beta Portal pairing of this server. The server polls the portal; this
 * store only asks the server, on the interval the server names, while a page
 * that shows the pairing is open. A paired server is asked to check its
 * pairing when the page opens and every minute after, so a pairing revoked
 * or suspended in the portal shows without an update check.
 */
export class PortalPairing {
  readonly #service: PortalPairingService;
  readonly #onPaired: (() => void) | undefined;
  readonly #setTimer: (run: () => void, ms: number) => unknown;
  readonly #clearTimer: (timer: unknown) => void;

  #status = $state.raw<PortalPairingStatus | undefined>(undefined);
  #error = $state.raw<DataFault | undefined>(undefined);
  #loaded = $state(false);
  #busy = $state<PortalPairingAction | undefined>(undefined);
  #opened = $state(false);
  #failures = $state(0);

  #flight: Promise<void> | undefined;
  #watchers = 0;
  #timer: unknown;
  #resets = 0;
  // A paired server is checked once as soon as a page shows it, then every
  // VERIFY_MS while the page stays open.
  #verified = false;

  readonly #phase = $derived.by((): PortalPairingPhase => {
    if (!this.#loaded) return 'loading';
    const code = this.#error?.code;
    if (code === CLOCK_SKEW_FAULT) return 'clock';
    const state = this.#status?.state;
    if (state === 'paired') return 'paired';
    if (state === 'pending') {
      if (this.#status?.step === 'awaiting_approval') return 'approval';

      return this.#opened ? 'confirm' : 'code';
    }
    if (code === PORTAL_UNAVAILABLE_FAULT || code === NETWORK_FAULT)
      return 'unreachable';
    if (state === 'expired' || state === 'denied' || state === 'revoked')
      return state;

    return 'intro';
  });

  readonly #pollDelay = $derived(
    Math.min(
      (this.#status?.interval_ms ?? DEFAULT_POLL_MS) * 2 ** this.#failures,
      MAX_POLL_MS,
    ),
  );

  constructor(
    service: PortalPairingService,
    options: PortalPairingOptions = {},
  ) {
    this.#service = service;
    this.#onPaired = options.onPaired;
    this.#setTimer =
      options.setTimer ?? ((run, ms) => globalThis.setTimeout(run, ms));
    this.#clearTimer =
      options.clearTimer ??
      ((timer) => {
        globalThis.clearTimeout(timer as ReturnType<typeof setTimeout>);
      });
  }

  /** The server's answer. Read-only by design: only the server changes it. */
  get status(): PortalPairingStatus | undefined {
    return this.#status;
  }
  /** The last refusal, kept by its code so a screen can translate it. */
  get error(): DataFault | undefined {
    return this.#error;
  }
  get loaded(): boolean {
    return this.#loaded;
  }
  get busy(): PortalPairingAction | undefined {
    return this.#busy;
  }
  /** Whether the person opened the portal for the code on screen. */
  get opened(): boolean {
    return this.#opened;
  }
  get phase(): PortalPairingPhase {
    return this.#phase;
  }
  /** How long the store waits before it asks the server again. */
  get pollDelayMs(): number {
    return this.#pollDelay;
  }

  load(): Promise<void> {
    if (this.#flight) return this.#flight;
    const reset = this.#resets;
    const flight = this.#service
      .getPortalPairingStatus()
      .then((answer) => {
        if (reset !== this.#resets) return;
        this.#accept(answer);
      })
      .catch((thrown: unknown) => {
        if (reset === this.#resets) this.#error = faultOf(thrown);
      })
      .finally(() => {
        if (reset === this.#resets) this.#loaded = true;
        if (this.#flight === flight) this.#flight = undefined;
        this.#schedule();
      });
    this.#flight = flight;

    return flight;
  }

  /** Asks for a code. A refusal because the server is paired shows the pairing. */
  async start(): Promise<void> {
    this.#opened = false;
    await this.#act('start', () => this.#service.startPortalPairing());
    if (this.#error?.code === ALREADY_PAIRED_FAULT) {
      this.#error = undefined;
      await this.load();
    }
  }

  /**
   * Asks the server again. A pairing in progress refreshes it; anything else
   * starts one, so the same button answers a clock or a portal that failed.
   */
  async retry(): Promise<void> {
    if (this.#status?.state !== 'pending') {
      await this.start();

      return;
    }
    await this.#act('retry', () => this.#service.refreshPortalPairing());
  }

  /** One poll. A second call while one is on the way returns the same one. */
  refresh(): Promise<void> {
    if (this.#flight) return this.#flight;
    const reset = this.#resets;
    const flight = this.#service
      .refreshPortalPairing()
      .then((answer) => {
        if (reset !== this.#resets) return;
        this.#accept(answer);
      })
      .catch((thrown: unknown) => {
        if (reset !== this.#resets) return;
        this.#error = faultOf(thrown);
        this.#failures += 1;
      })
      .finally(() => {
        if (this.#flight === flight) this.#flight = undefined;
        this.#schedule();
      });
    this.#flight = flight;

    return flight;
  }

  /** Ends the pairing on this server, whether it is pending or working. */
  async disconnect(): Promise<void> {
    this.#opened = false;
    await this.#act(
      'disconnect',
      () => this.#service.disconnectPortal(),
      (fault) => {
        // The server clears its own pairing before it asks the portal, so
        // only an unconfirmed revocation leaves it unpaired. After any other
        // refusal the store asks the server what it holds.
        if (fault.code === REVOCATION_INCOMPLETE_FAULT) {
          this.#status = { state: 'unpaired' };
        } else {
          void this.load();
        }
      },
    );
  }

  /** The person opened the portal for the code on screen. */
  markOpened(): void {
    if (this.#status?.state === 'pending') this.#opened = true;
  }

  /**
   * Keeps polling while the caller shows the pairing. The returned function
   * stops it. Two pages showing it share the one timer.
   */
  watch(): () => void {
    this.#watchers += 1;
    this.#schedule();
    let stopped = false;

    return () => {
      if (stopped) return;
      stopped = true;
      this.#watchers -= 1;
      if (this.#watchers === 0) this.#cancel();
    };
  }

  reset(): void {
    this.#resets += 1;
    this.#cancel();
    this.#flight = undefined;
    this.#status = undefined;
    this.#error = undefined;
    this.#loaded = false;
    this.#busy = undefined;
    this.#opened = false;
    this.#failures = 0;
    this.#verified = false;
  }

  #accept(answer: PortalPairingStatus): void {
    // Only a pairing finishing here calls onPaired. A page that loads an
    // existing pairing has read everything it needs already.
    const finished =
      answer.state === 'paired' && this.#status?.state === 'pending';
    if (answer.state !== 'pending') this.#opened = false;
    this.#status = answer;
    this.#error = undefined;
    this.#failures = 0;
    this.#loaded = true;
    if (finished) this.#onPaired?.();
  }

  // One action the person pressed. An answer that arrives after a reset is
  // dropped, the same as a late load.
  async #act(
    action: PortalPairingAction,
    request: () => Promise<PortalPairingStatus>,
    failed?: (fault: DataFault) => void,
  ): Promise<void> {
    if (this.#busy !== undefined) return;
    const reset = this.#resets;
    this.#busy = action;
    try {
      const answer = await request();
      if (reset === this.#resets) this.#accept(answer);
    } catch (thrown) {
      if (reset === this.#resets) {
        const fault = faultOf(thrown);
        this.#error = fault;
        failed?.(fault);
      }
    } finally {
      if (reset === this.#resets) this.#busy = undefined;
      this.#schedule();
    }
  }

  #schedule(): void {
    this.#cancel();
    if (this.#watchers === 0) return;
    const state = this.#status?.state;
    if (state !== 'pending' && state !== 'paired') return;
    let delay = this.#pollDelay;
    if (state === 'paired') delay = this.#verified ? VERIFY_MS : 0;
    this.#timer = this.#setTimer(() => {
      this.#timer = undefined;
      if (state === 'paired') this.#verified = true;
      void this.refresh();
    }, delay);
  }

  #cancel(): void {
    if (this.#timer === undefined) return;
    this.#clearTimer(this.#timer);
    this.#timer = undefined;
  }
}
