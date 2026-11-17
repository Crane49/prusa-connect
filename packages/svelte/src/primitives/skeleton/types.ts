import type { ClassValue, HTMLAttributes } from 'svelte/elements';

export interface SkeletonProps extends Omit<
  HTMLAttributes<HTMLDivElement>,
  'class' | 'children'
> {
  class?: ClassValue | undefined;
}
