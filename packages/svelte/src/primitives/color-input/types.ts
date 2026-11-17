import type { ClassValue } from 'svelte/elements';

export interface ColorInputProps {
  /** A `#RRGGBB` colour. */
  value: string;
  /** Reports the chosen colour as upper-case `#RRGGBB`. */
  oninput: (hex: string) => void;
  /** The input's accessible name. */
  label: string;
  class?: ClassValue | undefined;
}
