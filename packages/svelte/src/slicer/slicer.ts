// The messages between a page and PrusaSlicer, when the page is shown inside
// the slicer's webview (ADR-20261112-2eb rules 4 and 5).
//
// The slicer puts `window._prusaSlicer.postMessage` on the page and calls
// functions on `window._prusaConnect_v2` to drive it. The names and the
// shapes here are the ones the slicer sends and reads.
//
// The core app installs the functions and turns each call into a window
// event. A module page reads those events through `slicer.on` and talks back
// through `slicer.post`.

/** A material name, as the slicer writes it: `PLA`, `PETG`. */
export type SlicerMaterial = string;

/** What the page sends the slicer. Each `action` is one the slicer reads. */
export type SlicerMessage =
  /** Asks the slicer to call `init` again. In Prusa Connect mode it opens a new page session first. */
  | { action: 'REQUEST_CONFIG' }
  /** The person picked a printer on the page. */
  | { action: 'SELECT_PRINTER'; uuid: string }
  /** The page is drawn and listens for calls. */
  | { action: 'WEBAPP_READY'; componentName?: string }
  /** Opens an address in the system browser instead of the webview. */
  | { action: 'REQUEST_OPEN_IN_BROWSER'; url: string }
  /** Closes the slicer's dialog that shows the page. */
  | { action: 'CLOSE_DIALOG' }
  /** Downloads a file the slicer fetches with its own token. */
  | { action: 'REQUEST_FILE_DOWNLOAD'; name: string; url: string }
  /** Sends the sliced file to a printer. */
  | ({ action: 'PRINT'; data: SlicerPrintData } & SlicerPrintData);

/** The fields of a `PRINT` message. The slicer reads them flat and under `data`. */
export interface SlicerPrintData {
  set_ready: boolean;
  /** Position in the queue: -1 last, 0 first. */
  position?: number;
  /** When to start, in Unix seconds. */
  wait_until: number | undefined;
  /** The file name without a folder. */
  filename: string;
  /** The storage and folder. The server picks them when this is missing. */
  destination?: string;
  printer_uuid: string;
  team_id: number;
}

/** What the slicer passes to `init`. */
/** What `init` passes on to a page. The slicer's credential is never among it. */
export interface SlicerInitOptions {
  /** The slicer's language, such as `cs_CZ`. The page reads only the part before `_`. */
  language?: string;
  clientVersion?: string;
  colorMode?: 'LIGHT' | 'DARK';
}

/** What the slicer passes to `requestCompatiblePrinter`. */
export interface CompatiblePrinterParams {
  printerUuid?: string;
  filename?: string;
  /** The printer type in dot notation, such as `1.3.1`. */
  printerType?: string;
  /** The model name, such as `MK4S` or `XL`. */
  printerModel?: string;
  /** One name, one name per tool, or names joined with `;`. */
  material?: SlicerMaterial | SlicerMaterial[];
  nozzleDiameter?: number;
  /** Per tool: the nozzle takes abrasive material. */
  filament_abrasive?: boolean[];
  /** Per tool: the print needs a high-flow nozzle. */
  high_flow?: boolean[];
  nozzle_diameter?: number[];
  /** The project has more than one bed. The slicer sends the first and queues the rest. */
  multiple_beds?: boolean;
}

/** The window events the core app sends for each call of the slicer. */
export interface SlicerEvents {
  /**
   * `init`, from both API versions. No page receives the slicer's
   * credential (ADR-20261112-nay).
   */
  slicerinit: SlicerInitOptions;
  /** `logout`. */
  logout: null;
  /** `requestCompatiblePrinter`. */
  requestcompatibleprinter: CompatiblePrinterParams;
}

export type SlicerEventName = keyof SlicerEvents;

/** The `detail` of every slicer event. */
export interface SlicerEventDetail<T> {
  apiVersion: number;
  data: T;
}

/** What the slicer puts on the page. */
export interface SlicerHost {
  postMessage(message: SlicerMessage): void;
}

/** The part of `window` the helper reads. A test hands in its own. */
export interface SlicerWindow {
  _prusaSlicer?: Partial<SlicerHost> | undefined;
  addEventListener(type: string, listener: (event: Event) => void): void;
  removeEventListener(type: string, listener: (event: Event) => void): void;
  dispatchEvent(event: Event): boolean;
}

/** What a page uses to talk to PrusaSlicer. */
export interface Slicer {
  /** True when the page is shown inside PrusaSlicer. */
  readonly available: boolean;
  /** Sends a message to the slicer. False when the page is not inside it. */
  post(message: SlicerMessage): boolean;
  /**
   * Calls `listener` with the data of each event of this name. The returned
   * function stops it. Outside the slicer no event ever arrives.
   */
  on<Name extends SlicerEventName>(
    name: Name,
    listener: (data: SlicerEvents[Name], apiVersion: number) => void,
  ): () => void;
  /** Sends the event `name`, as the core app does for each call of the slicer. */
  emit<Name extends SlicerEventName>(
    name: Name,
    data: SlicerEvents[Name],
    apiVersion?: number,
  ): void;
}

const hostOf = (target: SlicerWindow): SlicerHost | undefined => {
  const host = target._prusaSlicer;
  if (typeof host?.postMessage !== 'function') return undefined;
  const post = host.postMessage.bind(host);

  return { postMessage: post };
};

const detailOf = <T>(event: Event): SlicerEventDetail<T> | undefined => {
  if (!(event instanceof CustomEvent)) return undefined;
  // The detail is what `emit` wrote. A page that sends an event of the same
  // name with another shape is refused here.
  const detail: unknown = event.detail;
  if (
    typeof detail !== 'object' ||
    detail === null ||
    !('apiVersion' in detail) ||
    typeof detail.apiVersion !== 'number' ||
    !('data' in detail)
  ) {
    return undefined;
  }

  // Checked above as far as the shape goes; the data is the slicer's.
  return detail as SlicerEventDetail<T>;
};

/**
 * A helper over one window. `target` is read on each call, so the helper may
 * be built where there is no window, such as under SSR.
 */
export const createSlicer = (
  target: () => SlicerWindow | undefined,
): Slicer => ({
  get available() {
    const found = target();

    return found !== undefined && hostOf(found) !== undefined;
  },
  post(message) {
    const found = target();
    const host = found === undefined ? undefined : hostOf(found);
    if (host === undefined) return false;
    host.postMessage(message);

    return true;
  },
  on(name, listener) {
    const found = target();
    // Nothing to listen on, so there is nothing to stop.
    if (found === undefined) return () => undefined;
    const handle = (event: Event): void => {
      const detail = detailOf<SlicerEvents[typeof name]>(event);
      if (detail !== undefined) listener(detail.data, detail.apiVersion);
    };
    found.addEventListener(name, handle);

    return () => {
      found.removeEventListener(name, handle);
    };
  },
  emit(name, data, apiVersion = 1) {
    const detail: SlicerEventDetail<typeof data> = { apiVersion, data };
    target()?.dispatchEvent(new CustomEvent(name, { detail }));
  },
});

// Under SSR there is no window, and `globalThis` then has no
// `addEventListener`.
const pageWindow = (): SlicerWindow | undefined => {
  const page: Partial<SlicerWindow> = globalThis;

  return typeof page.addEventListener === 'function'
    ? // Checked: the page's own window.
      (page as SlicerWindow)
    : undefined;
};

/**
 * The page's own helper. A module page imports it from
 * `@prusa/connect-ui/slicer`, and the import map gives every bundle the
 * same one.
 *
 * ```ts
 * import { slicer } from '@prusa/connect-ui/slicer';
 *
 * if (slicer.available) {
 *   const stop = slicer.on('requestcompatibleprinter', (params) => pick(params));
 *   slicer.post({ action: 'WEBAPP_READY', componentName: 'spools' });
 * }
 * ```
 */
export const slicer: Slicer = createSlicer(pageWindow);
