import type { ClassValue } from 'svelte/elements';

/** What the swatch needs of a filament, and nothing more. */
export interface SwatchMaterial {
  name: string;
  /** `#RRGGBB`. A value from data, so it reaches the DOM through `style`, never a class. */
  colorRgba?: string | undefined;
  colorName?: string | undefined;
}

export interface MaterialSwatchProps {
  material: SwatchMaterial;
  /** The box's size, from the caller. */
  class?: ClassValue | undefined;
}
