import type { Snippet } from 'svelte';
import type { ClassValue } from 'svelte/elements';

import type { PageSize } from './sizes';

/**
 * The page state a listing hands the pager, as one named shape rather than
 * five loose props, so a screen can pass it down through a render helper
 * without restating it.
 */
export interface PagerControls {
  /** Zero-based, the way the listings hold it. The pager renders it one-based. */
  page: number;
  pageCount: number;
  pageSize: PageSize;
  onPageChange: (next: number) => void;
  onPageSizeChange: (next: PageSize) => void;
}

export interface PagerProps extends PagerControls {
  /**
   * What the listing counts, as the word that suits `total`: a job, four
   * jobs. The plural is the caller's, because only the caller knows the noun
   * and English is not the only language reading it. Ignored when `count` is
   * given.
   */
  unit?: string | undefined;
  total?: number | undefined;
  /**
   * Replaces the built-in sentence for a listing whose count says more than a
   * total, a catalogue naming the search term and the size of the corpus
   * behind it. It carries its own `aria-live`, since it is then the only
   * thing announcing that the page moved.
   */
  count?: Snippet | undefined;
  class?: ClassValue | undefined;
}
