import type { StepListState } from './types';

// Static strings chosen by key (svelte-5.md). A done step is a filled circle
// with a tick, the current one is a ring in the product's colour, and an
// upcoming one is an outline.
export const markers: Record<StepListState, string> = {
  done: 'border-primary bg-primary text-primary-foreground',
  current: 'border-primary text-link',
  upcoming: 'border-border text-muted-foreground',
};

export const labels: Record<StepListState, string> = {
  done: 'text-foreground',
  current: 'font-medium text-foreground',
  upcoming: 'text-muted-foreground',
};

// The line below a marker. It takes the product's colour once the step it
// leaves is done, so the part of the way behind the person reads as filled.
export const lines: Record<StepListState, string> = {
  done: 'bg-primary',
  current: 'bg-border',
  upcoming: 'bg-border',
};
