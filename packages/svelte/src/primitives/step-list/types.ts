import type { ClassValue } from 'svelte/elements';

/** Where a step stands: behind the person, under them, or ahead of them. */
export type StepListState = 'done' | 'current' | 'upcoming';

export interface StepListStep {
  id: string;
  /** The short name of the step. */
  label: string;
  /** One muted line under the label, such as what the step asks for. */
  description?: string | undefined;
}

export interface StepListProps {
  /** The name the list carries, for a screen reader. */
  label: string;
  steps: readonly StepListStep[];
  /** The step the person is on, counted from zero. */
  current: number;
  /**
   * The furthest step the person has reached, counted from zero. A step
   * ahead of `current` and up to this one can be picked too. Defaults to
   * `current`.
   */
  reached?: number | undefined;
  /**
   * Opens a step the person has already passed or reached. Without it the
   * list only shows where the person is, and no step is a button.
   */
  onpick?: ((id: string) => void) | undefined;
  /** Draws a thin line from each marker down to the next one. */
  connected?: boolean | undefined;
  class?: ClassValue | undefined;
}
