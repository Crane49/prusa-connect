import { getContext, setContext } from 'svelte';

const key = Symbol('facet-popover-close');

/**
 * Lets the list inside shut the popover it is drawn in. A single-choice facet
 * needs it, because picking one value is the whole interaction and leaving the
 * panel open afterwards makes it behave unlike every other select on the
 * screen. A multi-choice list ignores it and stays open, which is what makes
 * picking several values in a row possible.
 */
export const provideFacetPopoverClose = (close: () => void): void => {
  setContext(key, close);
};

/** Shuts the facet popover this component is drawn in, or does nothing outside one. */
export const facetPopoverClose = (): (() => void) =>
  getContext<(() => void) | undefined>(key) ?? (() => undefined);
