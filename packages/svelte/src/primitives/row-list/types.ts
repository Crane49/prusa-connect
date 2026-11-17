import type { Snippet } from 'svelte';
import type { ClassValue, HTMLAttributes } from 'svelte/elements';

export interface RowListProps extends Omit<
  HTMLAttributes<HTMLDivElement>,
  'class' | 'children'
> {
  /** The rows; each draws its own separator. */
  children: Snippet;
  class?: ClassValue | undefined;
}
