// Which drawing of a printer a project's printer model asks for.
import {
  PRINTER_MODELS,
  type PrinterModel,
} from '@prusa/connect-ui/primitives';

const MODEL_BY_SPELLING: ReadonlyMap<string, PrinterModel> = new Map(
  PRINTER_MODELS.map((model) => [model.replaceAll('_', ''), model]),
);

/**
 * The drawing for one printer model, as a slicer spells it. `COREONE`,
 * `Core One` and `CoreOne` are one machine, so the name is folded to letters
 * and digits before the lookup. A spelling no drawing answers is `unknown`,
 * which has a drawing of its own.
 */
export const printerModelOf = (model: string): PrinterModel =>
  MODEL_BY_SPELLING.get(model.toLowerCase().replace(/[^a-z0-9]/g, '')) ??
  'unknown';
