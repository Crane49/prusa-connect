// What this page does when PrusaSlicer shows it (ADR-20261112-2eb rules 4
// and 5).
//
// The slicer puts `window._prusaSlicer.postMessage` on the page before the
// page's scripts run, and drives the page by calling functions on
// `window._prusaConnect_v1` and `window._prusaConnect_v2`. The page installs
// them with the names and the arguments the slicer calls.
// A page with no `_prusaSlicer` installs nothing.
//
// The page never receives the slicer's credential (ADR-20261112-nay). The
// slicer opens a page session itself and puts its HttpOnly cookie into the
// web view, so every request of the page signs itself with the cookie and no
// script on the page, a module bundle included, can read the credential. An
// access token the slicer still passes to `init` is dropped here: it is not
// kept, logged, or sent on in any event.
import type {
  CompatiblePrinterParams,
  Slicer,
  SlicerInitOptions,
  SlicerWindow,
} from '@prusa/connect-ui/slicer';

import { log } from '../log';
import { slicerLanguage } from './language';

const bridgeLog = log.scope('slicer');

/**
 * How long after the page session was confirmed the page asks the slicer to
 * open a new one. The core's page session lives an hour, so the new one is in
 * place before the old one ends and no request meets a 401.
 */
export const REFRESH_MS = 45 * 60 * 1000;

/**
 * How long the page waits for the slicer to answer a REQUEST_CONFIG before a
 * later 401 or refresh may ask again. A slicer that dropped the message would
 * otherwise never be asked again.
 */
export const ASK_MS = 30 * 1000;

/** The functions installed as `window._prusaConnect_v1`. */
export interface SlicerApiV1 {
  init(options: SlicerInitCall): void;
  setAccessToken(token: string): void;
  setSessionId(sessionId: string): void;
  requestCompatiblePrinter(params: CompatiblePrinterParams): void;
  logout(): void;
}

/** The functions installed as `window._prusaConnect_v2`. */
export interface SlicerApiV2 {
  init(options: SlicerInitCall): void;
  requestCompatiblePrinter(params: CompatiblePrinterParams): void;
  logout(): void;
}

/** The page's window, as the bridge writes to it. */
export interface BridgeWindow extends SlicerWindow {
  _prusaConnect_v1?: SlicerApiV1;
  _prusaConnect_v2?: SlicerApiV2;
}

/** The language and the theme the slicer asked for. */
export interface SlicerLook {
  language?: ReturnType<typeof slicerLanguage>;
  dark?: boolean;
}

/** Runs `run` after `ms`, and answers a function that cancels it. */
export type Schedule = (run: () => void, ms: number) => () => void;

export interface BridgeOptions {
  page: BridgeWindow;
  slicer: Slicer;
  reload: () => void;
  /** Applies the language and the theme the slicer asked for. */
  onLook: (look: SlicerLook) => void;
  /** Ends the page session the cookie holds, on the slicer's logout. */
  endSession: () => Promise<void>;
  /** Runs a timer. A test hands in its own. */
  schedule?: Schedule;
}

const timer: Schedule = (run, ms) => {
  const id = setTimeout(run, ms);

  return () => {
    clearTimeout(id);
  };
};

/** What the slicer passes to `init`, its credential included. */
type SlicerInitCall = SlicerInitOptions & {
  accessToken?: string;
  sessionId?: string;
};

/**
 * What `init` passes on to the page: the fields a page reads and nothing
 * else, so neither the credential nor a field a later slicer adds reaches a
 * page script.
 */
const withoutCredential = (options: SlicerInitCall): SlicerInitOptions => {
  const data: SlicerInitOptions = {};
  if (options.language !== undefined) {
    data.language = slicerLanguage(options.language);
  }
  if (options.clientVersion !== undefined) {
    data.clientVersion = options.clientVersion;
  }
  if (options.colorMode !== undefined) data.colorMode = options.colorMode;

  return data;
};

/**
 * The bridge between the slicer and this page. One per page, built when the
 * page starts and kept for its life.
 */
export class SlicerBridge {
  readonly #options: BridgeOptions;
  readonly #schedule: Schedule;
  // A request of the page met a 401 since the page session was last
  // confirmed.
  #refused = false;
  // The slicer answered a REQUEST_CONFIG after a 401 and the page still has
  // no person: the slicer holds nothing this server takes, and the person
  // signs in again from the slicer.
  #stale = false;
  readonly #onStale = new Set<() => void>();
  // A REQUEST_CONFIG is out and the slicer has not answered yet. Calling it
  // stops waiting for the answer.
  #cancelAsk: (() => void) | undefined;
  // The one check of the page session in flight, which every caller joins.
  #checking: Promise<void> | undefined;
  #cancelRefresh: (() => void) | undefined;
  #signedIn: () => boolean = () => false;
  #recheck: () => Promise<boolean> = () => Promise.resolve(false);

  constructor(options: BridgeOptions) {
    this.#options = options;
    this.#schedule = options.schedule ?? timer;
  }

  /**
   * Installs the two global objects, listens for the slicer's calls and for
   * clicks on links that open a new window, and asks for the configuration.
   */
  install(): void {
    const { page, slicer } = this.#options;
    const emit = slicer.emit.bind(slicer);
    // Each event says which of the two objects the slicer called.
    const init =
      (apiVersion: number) =>
      (options: SlicerInitCall): void => {
        emit('slicerinit', withoutCredential(options), apiVersion);
      };
    const logout = (apiVersion: number) => (): void => {
      emit('logout', null, apiVersion);
    };

    // Fixed in place before any module bundle runs, so no script on the
    // page can wrap `init` and read what the slicer passes to it
    // (ADR-20261112-nay).
    const fixed = (name: string, value: SlicerApiV1 | SlicerApiV2): void => {
      Object.defineProperty(page, name, {
        value: Object.freeze(value),
        writable: false,
        configurable: false,
        enumerable: true,
      });
    };

    fixed('_prusaConnect_v1', {
      init: init(1),
      // The page holds no token. The call is accepted so a slicer that
      // makes it does not fail, and the token goes nowhere.
      setAccessToken: () => {
        bridgeLog.debug('setAccessToken ignored: the page holds no token');
      },
      // The core has no session id to set. The call is accepted so a slicer
      // that makes it does not fail.
      setSessionId: () => {
        bridgeLog.debug('setSessionId ignored: this server has no session id');
      },
      // PrusaSlicer 2.8.1 sends the two flags swapped. Version 1 swaps them
      // back.
      requestCompatiblePrinter: (params) => {
        const swapped: CompatiblePrinterParams = { ...params };
        if (params.filament_abrasive !== undefined) {
          swapped.high_flow = params.filament_abrasive;
        }
        if (params.high_flow !== undefined) {
          swapped.filament_abrasive = params.high_flow;
        }
        emit('requestcompatibleprinter', swapped);
      },
      logout: logout(1),
    } satisfies SlicerApiV1);
    fixed('_prusaConnect_v2', {
      init: init(2),
      requestCompatiblePrinter: (params) => {
        emit('requestcompatibleprinter', params, 2);
      },
      logout: logout(2),
    } satisfies SlicerApiV2);

    // Page-lifetime listeners: the bridge lives as long as the page.
    slicer.on('slicerinit', (options) => {
      this.#configured(options);
    });
    slicer.on('logout', () => {
      void this.#logout();
    });
    page.addEventListener('click', (event) => {
      this.#click(event);
    });

    this.#ask();
  }

  /**
   * True once the slicer answered after a 401 and the page still has no
   * person. Waiting longer is then pointless: the person signs out and in
   * again from PrusaSlicer.
   */
  get stale(): boolean {
    return this.#stale;
  }

  /** Calls `listener` when `stale` turns true. */
  onStale(listener: () => void): void {
    this.#onStale.add(listener);
  }

  /**
   * Tells the bridge whether a person is signed in on the page now, and how
   * to ask the core again after the slicer set a new cookie.
   */
  watch(signedIn: () => boolean, recheck?: () => Promise<boolean>): void {
    this.#signedIn = signedIn;
    if (recheck !== undefined) this.#recheck = recheck;
  }

  /**
   * What a 401 from any request does inside the slicer: the page session
   * ended, so the page asks the slicer to open a new one. A 401 while no
   * person is signed in ends no page session: it is the page's first check
   * before the slicer paired, or the bridge's own check after the slicer
   * answered, and counting it would ask the slicer again without end.
   */
  refused(): void {
    if (!this.#signedIn()) return;
    this.#refused = true;
    this.#ask();
  }

  #ask(): void {
    if (this.#cancelAsk !== undefined) return;
    if (!this.#options.slicer.post({ action: 'REQUEST_CONFIG' })) return;
    this.#cancelAsk = this.#schedule(() => {
      this.#cancelAsk = undefined;
    }, ASK_MS);
  }

  #configured(options: SlicerInitOptions): void {
    this.#cancelAsk?.();
    this.#cancelAsk = undefined;
    const look: SlicerLook = {};
    if (options.language !== undefined) {
      look.language = slicerLanguage(options.language);
    }
    if (options.colorMode !== undefined) {
      look.dark = options.colorMode === 'DARK';
    }
    if (look.language !== undefined || look.dark !== undefined) {
      this.#options.onLook(look);
    }
    void this.#check();
  }

  // Asks the core whether the cookie the slicer set holds a person. Calls
  // that arrive while one check is in flight join it.
  #check(): Promise<void> {
    this.#checking ??= this.#confirm().finally(() => {
      this.#checking = undefined;
    });

    return this.#checking;
  }

  async #confirm(): Promise<void> {
    const signedIn = this.#signedIn() || (await this.#recheck());
    if (!signedIn) {
      // Only an answer to a 401 says the slicer has nothing: on a first
      // visit the slicer may still be pairing, and the page waits.
      if (this.#refused) this.#markStale();

      return;
    }

    const wasRefused = this.#refused;
    this.#refused = false;
    this.#stale = false;
    this.#cancelRefresh?.();
    this.#cancelRefresh = this.#schedule(() => {
      this.#ask();
    }, REFRESH_MS);
    if (wasRefused) {
      // Requests that met the 401 left parts of the page empty.
      bridgeLog.info('reloading after the slicer opened a new page session');
      this.#options.reload();
    }
  }

  #markStale(): void {
    if (this.#stale) return;
    bridgeLog.info('the slicer holds no page session this server takes');
    this.#stale = true;
    for (const listener of this.#onStale) listener();
  }

  async #logout(): Promise<void> {
    const had = this.#signedIn();
    this.#cancelRefresh?.();
    this.#cancelRefresh = undefined;
    await this.#options.endSession();
    // A page that drew for a person drops what it holds for them.
    if (had) this.#options.reload();
  }

  #click(event: Event): void {
    if (!(event.target instanceof Element)) return;
    const anchor = event.target.closest('a');
    if (anchor?.getAttribute('target') !== '_blank') return;
    if (anchor.getAttribute('href') === null) return;
    event.preventDefault();
    // The slicer hands the address to the system, which would run a
    // `file:` or a custom scheme as readily as a web page. Only a web
    // address leaves the page.
    if (anchor.protocol !== 'http:' && anchor.protocol !== 'https:') return;
    // `anchor.href` is the address resolved against the page, so a relative
    // link opens on this server in the system browser.
    this.#options.slicer.post({
      action: 'REQUEST_OPEN_IN_BROWSER',
      url: anchor.href,
    });
  }
}

let current: SlicerBridge | undefined;

/**
 * Builds and installs the bridge when the slicer shows this page, and answers
 * it. On any other page it does nothing and answers `undefined`.
 */
export const startSlicerBridge = (
  options: BridgeOptions,
): SlicerBridge | undefined => {
  if (!options.slicer.available) return undefined;
  current = new SlicerBridge(options);
  current.install();

  return current;
};

/** The bridge this page installed, or `undefined` outside the slicer. */
export const slicerBridge = (): SlicerBridge | undefined => current;
