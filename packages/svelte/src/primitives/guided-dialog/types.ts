import type { Snippet } from 'svelte';
import type { ClassValue } from 'svelte/elements';

import type { StepperStep } from '../stepper/types';

/**
 * Where the flow stands. `editing` is a step asking a question, `waiting` is
 * the flow watching for something outside it, and the last two are the ends it
 * reaches.
 */
export type GuidedStatus = 'editing' | 'waiting' | 'success' | 'failure';

export interface GuidedDialogProps {
  open?: boolean;
  /** The name of the whole flow, such as "Add a device". */
  title: string;
  description?: string | undefined;
  steps: readonly StepperStep[];
  /** The step on screen, counted from zero. The caller owns it. */
  current: number;
  status?: GuidedStatus;
  /** Whether this step has what it needs for the primary button to work. */
  canContinue?: boolean;
  /** The primary button is busy: it reports `aria-busy` and swallows clicks. */
  busy?: boolean;
  /** What the flow is waiting for, in one sentence. */
  waitingMessage?: string | undefined;
  /** The heading of the success or the failure screen. */
  endTitle?: string | undefined;
  /** What happened and what to do about it, in one or two sentences. */
  endMessage?: string | undefined;
  backLabel?: string;
  nextLabel?: string;
  /** The primary button on the last step. */
  finishLabel?: string;
  /** The one button an end screen offers when `endActions` is absent. */
  doneLabel?: string;
  /** Pressing Back. The caller lowers `current` itself. */
  onback?: (() => void) | undefined;
  /** Pressing the primary button. The caller raises `current` itself. */
  onnext?: (() => void) | undefined;
  /** The dialog closed, by Escape, by the scrim, or by a close control. */
  onclose?: (() => void) | undefined;
  /** The body of the step on screen. */
  children?: Snippet;
  /** A link to the page that explains this failure, or this wait. */
  help?: Snippet;
  /** What an end screen offers instead of the one Done button. */
  endActions?: Snippet;
  class?: ClassValue | undefined;
}

export interface GuidedEndProps {
  status: 'success' | 'failure';
  title?: string | undefined;
  message?: string | undefined;
  help?: Snippet | undefined;
}
