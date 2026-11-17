import type { ClassValue, HTMLAttributes } from 'svelte/elements';

/** Which way the line runs. */
export type SeparatorOrientation = 'horizontal' | 'vertical';

export interface SeparatorProps extends Omit<
  HTMLAttributes<HTMLDivElement>,
  'class' | 'children'
> {
  orientation?: SeparatorOrientation;
  /**
   * The line is a drawing and says nothing a person needs read out. This is
   * the default. A line that marks a real break, such as the one between two
   * groups in a menu, sets `false` and is announced as a separator.
   */
  decorative?: boolean;
  class?: ClassValue | undefined;
}
