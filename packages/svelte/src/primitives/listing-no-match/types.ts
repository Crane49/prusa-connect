import type { ClassValue } from 'svelte/elements';

import type { EmptyStateHeadingLevel } from '../empty-state';

export interface ListingNoMatchProps {
  heading: string;
  description?: string | undefined;
  headingLevel: EmptyStateHeadingLevel;
  /** Empties the search and the filters, so every row is back. */
  onClear: () => void;
  /** A listing that searches but has no filters passes the narrower label. */
  clearLabel?: string | undefined;
  class?: ClassValue | undefined;
}
