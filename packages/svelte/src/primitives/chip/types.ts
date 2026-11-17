import type { Snippet } from 'svelte';
import type { ClassValue } from 'svelte/elements';

/** What a `child` snippet spreads onto its own element. */
export interface ChipPressProps {
  class: ClassValue;
  'aria-label': string | undefined;
  /** Set on a link or a `child`; a button gets `aria-pressed` instead. */
  'aria-current'?: 'page' | undefined;
  'aria-pressed'?: boolean;
}

export interface ChipProps {
  /** A chip with an href is a link; without one it is a button. */
  href?: string | undefined;
  onclick?: (() => void) | undefined;
  /** Link: `aria-current="page"`. Button: `aria-pressed`. */
  selected?: boolean | undefined;
  count?: number | undefined;
  /** The accessible name, when the text and count alone do not say it. */
  label?: string | undefined;
  /**
   * Draws the chip's own element in place of the default one, so a caller can
   * render its router link with the chip's look and ARIA attributes.
   * The caller draws the content, count included.
   */
  child?: Snippet<[{ props: ChipPressProps }]> | undefined;
  children?: Snippet | undefined;
  class?: ClassValue | undefined;
}
