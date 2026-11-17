import type { Snippet } from 'svelte';
import type { ClassValue, HTMLLabelAttributes } from 'svelte/elements';

/**
 * `default` is the name above a control. `option` sits beside a checkbox or
 * radio: normal weight, start-aligned, snug lines. `hint` is the small muted
 * label over a dense input.
 */
export type LabelVariant = 'default' | 'option' | 'hint';

export interface LabelProps extends Omit<
  HTMLLabelAttributes,
  'class' | 'children'
> {
  variant?: LabelVariant;
  class?: ClassValue | undefined;
  children?: Snippet;
}
