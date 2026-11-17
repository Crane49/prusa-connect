import type { ClassValue, HTMLAttributes } from 'svelte/elements';

export interface CopyFieldProps extends Omit<
  HTMLAttributes<HTMLDivElement>,
  'class' | 'children'
> {
  /** The name of the value, drawn above it and tied to the input. */
  label: string;
  /**
   * The value itself. It is drawn in an input rather than as text so a person
   * can select it by hand, which is the way out when the clipboard refuses.
   */
  value: string;
  class?: ClassValue | undefined;
}
