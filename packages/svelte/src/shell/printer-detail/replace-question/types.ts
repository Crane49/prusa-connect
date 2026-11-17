export interface ReplaceQuestionProps {
  /** The name the printer already holds. Undefined closes the question. */
  name: string | undefined;
  /** True when the file is to be printed once it replaces the other. */
  print?: boolean | undefined;
  onReplace: () => void;
  /** Prints the file the printer already holds and sends nothing, for a print. */
  onPrintHeld?: (() => void) | undefined;
  onKeep: () => void;
}
