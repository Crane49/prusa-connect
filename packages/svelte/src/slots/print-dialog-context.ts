// What the print dialog tells a `print-section` drawn inside it: the printers
// a person ticked, and a way to close the dialog. Context and not a prop,
// because the section's props are the slot's subject alone (ADR-20260911-4p6 rule 10).
import { getContext, setContext } from 'svelte';

/** The print dialog, as a section drawn inside it reads it. */
export interface PrintDialogContext {
  /** The ids of the printers ticked in the dialog, in the order they were ticked. */
  readonly selected: readonly string[];
  /** Closes the dialog, as its Cancel button does. */
  close(): void;
}

const PRINT_DIALOG = Symbol('prusa.print-dialog');

// What a section reads outside a print dialog, such as in a module's own
// test: nothing ticked, and nothing to close.
const OUTSIDE: PrintDialogContext = {
  selected: [],
  close: () => undefined,
};

/** Called by the print dialog. The value's getters keep it reactive. */
export const setPrintDialogContext = (
  value: PrintDialogContext,
): PrintDialogContext => setContext(PRINT_DIALOG, value);

/**
 * The print dialog the calling component is drawn in. Call it while the
 * component initialises. Outside a dialog nothing is ticked and `close` does
 * nothing.
 */
export const printDialogContext = (): PrintDialogContext =>
  getContext<PrintDialogContext | undefined>(PRINT_DIALOG) ?? OUTSIDE;
