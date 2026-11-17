import type { StepperState } from './types';

// Static strings chosen by key (svelte-5.md). The indicator is the only part
// that changes colour: a done step is filled with the product's colour, the
// current one is the tinted panel with the colour's text, and an upcoming one
// is an outline.
export const indicators: Record<StepperState, string> = {
  done: 'border-primary bg-primary-fill text-primary-foreground',
  current: 'border-primary bg-secondary text-secondary-foreground',
  upcoming: 'border-border bg-transparent text-muted-foreground',
};

export const labels: Record<StepperState, string> = {
  done: 'text-muted-foreground',
  current: 'text-foreground',
  upcoming: 'text-muted-foreground',
};

// What a screen reader hears after the step's own name, because the colour and
// the tick say it to everyone else.
export const spoken: Record<StepperState, string> = {
  done: 'done',
  current: 'current step',
  upcoming: 'to do',
};
