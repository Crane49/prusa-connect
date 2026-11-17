import type { ClassValue } from 'svelte/elements';

/** Where a step stands: behind the person, under them, or ahead of them. */
export type StepperState = 'done' | 'current' | 'upcoming';

export interface StepperStep {
  id: string;
  /** The short name of the step, such as "Create your account". */
  label: string;
  /** One more line under the label. The compact form leaves it out. */
  description?: string | undefined;
}

export interface StepperProps {
  steps: readonly StepperStep[];
  /** The step the person is on, counted from zero. */
  current: number;
  /**
   * The form a dialog header uses: the numbers in a row, the labels read by a
   * screen reader alone, and one line saying which step of how many this is.
   */
  compact?: boolean;
  /** The name the list carries, for a screen reader. */
  label?: string;
  class?: ClassValue | undefined;
}

export interface StepperItemProps {
  step: StepperStep;
  /** The step's one-based number, which is what the indicator draws. */
  position: number;
  state: StepperState;
  compact?: boolean;
}
