// The print dialog PrusaSlicer shows. The slicer opens the page, the page
// says it is ready, and the slicer answers with the printer the file needs.
// The person picks a printer and the page tells the slicer where to send the
// file. The slicer uploads it itself. This is the cloud's SelectPrinterPage.
import { describeFault, type FaultText } from '@prusa/connect-ui/i18n';
import type { Slicer } from '@prusa/connect-ui/slicer';

import type { PrusaSlicerApi, SlicerPrinter } from './api';
import {
  type Candidate,
  candidatesFor,
  normalizeRequest,
  preselected,
  printMessage,
  type PrintRequest,
} from './compatibility';

/**
 * The wait after one read of the printers ends before the next starts, so a
 * printer that becomes free shows. A slow read on a small server delays the
 * next one and never piles up behind it.
 */
export const REFRESH_MS = 20_000;

/** How the last read of the printers ended. */
export type Load = 'loading' | 'ready' | 'failed' | 'refused';

/** What the page draws, and what a story hands it in place of the server. */
export interface PickerView {
  /** True when PrusaSlicer shows the page. */
  readonly inSlicer: boolean;
  readonly load: Load;
  readonly fault: FaultText | undefined;
  /** What the slicer asked for. Undefined until it has said. */
  readonly request: PrintRequest | undefined;
  /** The printers to offer, best first. Read-only by design. */
  readonly candidates: readonly Candidate[];
  /** True when the person may use no printer at all, of any model. */
  readonly noPrinters: boolean;
  readonly selected: string | undefined;
  /** The name the file gets on the printer. */
  filename: string;
  /** True once the print was handed to the slicer. */
  readonly sent: boolean;
  /**
   * True when Print may be pressed: a printer is picked and the file has a
   * name. With several beds the slicer names each file, so no name is needed.
   */
  readonly ready: boolean;
  /** Reads the printers again. */
  refresh(): Promise<void>;
  select(uuid: string): void;
  print(): void;
  cancel(): void;
}

const statusOf = (thrown: unknown): number | undefined =>
  typeof thrown === 'object' && thrown !== null && 'status' in thrown
    ? Number(thrown.status)
    : undefined;

/** A store one page owns, so a class (svelte-5.md). */
export class PrinterPicker implements PickerView {
  #load = $state<Load>('loading');
  #fault = $state<FaultText | undefined>(undefined);
  #printers = $state.raw<readonly SlicerPrinter[]>([]);
  #request = $state.raw<PrintRequest | undefined>(undefined);
  #chosen = $state<string | undefined>(undefined);
  #filename = $state('');
  #sent = $state(false);
  readonly #candidates = $derived(
    this.#request === undefined
      ? []
      : candidatesFor(this.#request, this.#printers),
  );
  // The person's choice while it can still print, else the one the slicer
  // named.
  readonly #selected = $derived.by(() => {
    const chosen = this.#candidates.find(
      (candidate) => candidate.printer.uuid === this.#chosen,
    );
    if (chosen?.printable === true) return chosen.printer.uuid;

    return this.#request === undefined
      ? undefined
      : preselected(this.#request, this.#candidates);
  });

  readonly #api: Pick<PrusaSlicerApi, 'listSlicerPrinters'>;
  readonly #slicer: Slicer;
  #timer: ReturnType<typeof setTimeout> | undefined;
  #stop: (() => void) | undefined;
  readonly #controller = new AbortController();

  constructor(api: Pick<PrusaSlicerApi, 'listSlicerPrinters'>, slicer: Slicer) {
    this.#api = api;
    this.#slicer = slicer;
  }

  get inSlicer(): boolean {
    return this.#slicer.available;
  }

  get load(): Load {
    return this.#load;
  }

  get fault(): FaultText | undefined {
    return this.#fault;
  }

  get request(): PrintRequest | undefined {
    return this.#request;
  }

  get candidates(): readonly Candidate[] {
    return this.#candidates;
  }

  get noPrinters(): boolean {
    return this.#printers.length === 0;
  }

  get selected(): string | undefined {
    return this.#selected;
  }

  get filename(): string {
    return this.#filename;
  }

  /** A draft the person types, so any text is kept; `print` checks it. */
  set filename(next: string) {
    this.#filename = next;
  }

  get sent(): boolean {
    return this.#sent;
  }

  /**
   * Listens for the slicer's request, tells the slicer the page is ready, and
   * reads the printers now and again REFRESH_MS after each read ends, until
   * `dispose`.
   */
  start(): void {
    this.#stop = this.#slicer.on('requestcompatibleprinter', (params) => {
      const request = normalizeRequest(params);
      this.#request = request;
      this.#filename = request.filename ?? '';
    });
    this.#slicer.post({ action: 'WEBAPP_READY', componentName: 'PrinterList' });
    void this.#poll();
  }

  /** Reads the printers, then waits REFRESH_MS and reads again. */
  async #poll(): Promise<void> {
    await this.refresh();
    if (this.#controller.signal.aborted) return;
    this.#timer = setTimeout(() => void this.#poll(), REFRESH_MS);
  }

  dispose(): void {
    clearTimeout(this.#timer);
    this.#stop?.();
    this.#controller.abort();
  }

  async refresh(): Promise<void> {
    try {
      const { printers } = await this.#api.listSlicerPrinters(
        {},
        this.#controller.signal,
      );
      this.#printers = printers;
      this.#load = 'ready';
      this.#fault = undefined;
    } catch (thrown) {
      if (this.#controller.signal.aborted) return;
      const status = statusOf(thrown);
      this.#fault = describeFault(thrown);
      // A list already drawn stays: one failed read is not a reason to take
      // the printers away under the person's pointer.
      if (this.#load === 'ready') return;
      this.#load = status === 401 || status === 403 ? 'refused' : 'failed';
    }
  }

  /** Picks a printer that can print now. Any other is refused. */
  select(uuid: string): void {
    const candidate = this.#candidates.find(
      (item) => item.printer.uuid === uuid,
    );
    if (candidate?.printable !== true) return;
    this.#chosen = uuid;
  }

  /** True when the print can be handed to the slicer. */
  get ready(): boolean {
    return (
      !this.#sent &&
      this.#selected !== undefined &&
      (this.#request?.multipleBeds === true || this.#filename.trim() !== '')
    );
  }

  /** Hands the slicer the printer and the file name, and closes its dialog. */
  print(): void {
    const printer = this.#selected;
    if (!this.ready || printer === undefined) return;
    this.#slicer.post(printMessage(printer, this.#filename.trim()));
    this.#slicer.post({ action: 'CLOSE_DIALOG' });
    this.#sent = true;
  }

  cancel(): void {
    this.#slicer.post({ action: 'CLOSE_DIALOG' });
  }
}
