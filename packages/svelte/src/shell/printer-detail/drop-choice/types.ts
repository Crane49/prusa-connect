import type { PrintTarget } from '../../print-picker';

export interface DropChoiceProps {
  /** The names of the files dropped on the printer. Undefined keeps it shut. */
  names: readonly string[] | undefined;
  /** The printer's name, for the title. */
  printer: string;
  /**
   * Whether the printer can start a print now, and why not, as the print
   * dialog grades it. Undefined offers no print.
   */
  printTarget: PrintTarget | undefined;
  /** Sends the files, and starts the one file once it lands when `print`. */
  onConfirm: (print: boolean) => void;
  /**
   * Closes the dialog without sending anything.
   */
  onCancel: () => void;
}
