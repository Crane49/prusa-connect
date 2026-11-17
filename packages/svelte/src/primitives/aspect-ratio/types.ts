import type { Snippet } from 'svelte';
import type { ClassValue, HTMLAttributes } from 'svelte/elements';

export interface AspectRatioProps extends Omit<
  HTMLAttributes<HTMLDivElement>,
  'class' | 'children'
> {
  /** Width divided by height: `16 / 9` for a camera frame, `1` for a square. */
  ratio?: number;
  class?: ClassValue | undefined;
  children?: Snippet;
}
