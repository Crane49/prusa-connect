import type { ClassValue } from 'svelte/elements';

import type { EmptyStateHeadingLevel } from '../empty-state';

export interface NoLongerHereProps {
  heading: string;
  description?: string | undefined;
  headingLevel: EmptyStateHeadingLevel;
  /** The one way out: back to where the object used to be listed. */
  linkLabel: string;
  /** A link when the way out is a page; `onLink` when it is an action. */
  href?: string | undefined;
  onLink?: (() => void) | undefined;
  class?: ClassValue | undefined;
}
