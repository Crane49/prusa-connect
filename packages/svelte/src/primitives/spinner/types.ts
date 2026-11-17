import type { ClassValue } from 'svelte/elements';

export type SpinnerSize = 'sm' | 'md' | 'lg';

/** `inherit` takes the colour of the surrounding text. */
export type SpinnerTone = 'primary' | 'muted' | 'inherit';

export interface SpinnerProps {
  size?: SpinnerSize | undefined;
  tone?: SpinnerTone | undefined;
  /** Layout only: `shrink-0`, margins. */
  class?: ClassValue | undefined;
}
