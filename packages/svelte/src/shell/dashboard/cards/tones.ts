// The looks a progress bar and a note tint pick, as static
// strings chosen by key (svelte-5.md). Tailwind reads this file, so a utility
// written here is emitted; one assembled at run time is not. The badge and
// the edge of a printer are the Printers screen's own, in
// shell/printers/presentation.ts.
import {
  PRINTER_STATE_PROGRESS_TONE,
  type PrinterState,
} from '../../../domain';
import type { ProgressTone } from '../../../primitives';

import type { Tone } from './config';

/** The colour of a progress bar for a state. */
export const progressTone = (state: PrinterState): ProgressTone =>
  PRINTER_STATE_PROGRESS_TONE[state];

/** The tint of a note. */
export const NOTE_TONE: Readonly<Record<Tone, string>> = {
  plain: '',
  active: 'border-state-active-edge bg-state-active text-state-active-text',
  done: 'border-state-done-edge bg-state-done text-state-done-text',
  idle: 'border-state-idle-edge bg-state-idle text-state-idle-text',
  attention:
    'border-state-attention-edge bg-state-attention text-state-attention-text',
  fault: 'border-state-fault-edge bg-state-fault text-state-fault-text',
};
