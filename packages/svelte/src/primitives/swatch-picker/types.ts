import type { ClassValue } from 'svelte/elements';

export interface Swatch {
  key: string;
  hex: string;
  /** The swatch's name, for assistive technology and the hover title. */
  label: string;
}

export interface SwatchPickerProps {
  swatches: readonly Swatch[];
  /** The picked swatch's key, undefined when none is picked. */
  value: string | undefined;
  onPick: (key: string | undefined) => void;
  /** The group's name. */
  label: string;
  /** Pressing the picked swatch again reports `undefined`. */
  deselectable?: boolean;
  size?: 'sm' | 'md';
  class?: ClassValue | undefined;
}
