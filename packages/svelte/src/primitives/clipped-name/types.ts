import type { ClassValue } from 'svelte/elements';

export interface ClippedNameProps {
  name: string;
  /**
   * How many characters stay at the end. Raise it for a name whose meaningful
   * suffix is longer than a version and an extension.
   */
  tailLength?: number;
  class?: ClassValue | undefined;
}
