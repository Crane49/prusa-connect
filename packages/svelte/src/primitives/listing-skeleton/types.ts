import type { ClassValue } from 'svelte/elements';

export interface ListingSkeletonProps {
  /** What is loading, as a sentence a screen reader reads out: `Loading jobs`. */
  label: string;
  /** Roughly what the listing will show. A page's worth, not the whole set. */
  rows?: number;
  /** For a listing whose rows start with a picture, a spool or a plate. */
  art?: boolean;
  class?: ClassValue | undefined;
}
