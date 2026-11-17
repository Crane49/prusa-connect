import type { RunningTransferRow } from '../../data/running-transfers.svelte';
import type { PrintDetails } from '../../data/services/storage';
import type {
  Device,
  PrintAction,
  PrinterState,
  StateTransition,
} from '../../domain';

/**
 * The file the picker is about. It is on a storage of this server, or it is
 * already on the printer when no storage is named.
 */
export interface PrintableFile {
  /** What the row shows: the last segment of the path. */
  readonly name: string;
  /** The storage this server declares the file on. Absent for a file the printer holds. */
  readonly storage?: string | undefined;
  /** The file's path inside that storage, or its path on the printer. */
  readonly source: string;
  /** The picture inside the file, absent for a file that carries none. */
  readonly thumbnailUrl?: string | undefined;
  /** What the slicer wrote into the file, absent for a file that said nothing. */
  readonly details?: PrintDetails | undefined;
}

/** One printer as the picker draws it, and whether it can take the file now. */
export interface PrintTarget {
  readonly id: string;
  readonly name: string;
  readonly model: string;
  readonly state: PrinterState;
  /** True when this printer can start the file now. */
  readonly ready: boolean;
  /** Why it cannot, as a catalogue key. Absent on a printer that can. */
  readonly reasonKey: string | undefined;
  /** The states that key names, for the line that lists them. */
  readonly reasonStates?: readonly string[] | undefined;
}

export interface PrintDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The file to print. Absent keeps the dialog shut. */
  file: PrintableFile | undefined;
  /** Every printer this server holds. The picker sorts and grades them. */
  devices: readonly Device[];
  /**
   * The moves between states each device kind declared. A kind that names the
   * start verb decides which of its own states a print begins from.
   */
  transitions?: readonly StateTransition[] | undefined;
  /** The printers held by another operator or by a command still running. */
  locked?: readonly string[] | undefined;
  /** The printers that answered and have no USB drive to put the file on. */
  noStorage?: readonly string[] | undefined;
  /**
   * Every transfer the server is running, newest first. A printer the file is
   * on its way to shows how far it got. Absent draws no progress.
   */
  transfers?: readonly RunningTransferRow[] | undefined;
  /** Sends the file. The caller supplies it, so a product may send it elsewhere first. */
  onPrint: PrintAction;
  /** Called with the printers' ids once every one of them has taken the file. */
  onPrinted?: ((deviceIds: readonly string[]) => void) | undefined;
}
