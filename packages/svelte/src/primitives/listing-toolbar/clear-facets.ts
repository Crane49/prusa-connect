import type { FacetDimension } from '../listing-state/types';

/** Lets go of every value picked in every facet. */
export const clearFacets = (facets: readonly FacetDimension[]): void => {
  for (const facet of facets) {
    if (facet.selected.length > 0) facet.onSelectedChange([]);
  }
};
