import type { Snippet } from 'svelte';
import type { ClassValue } from 'svelte/elements';

import type { ListField, VisibleFields } from '../field-picker/types';
import type { ListingView } from '../listing-controls/types';
import type { FacetDimension } from '../listing-state/types';

/** A yes-or-no filter, drawn as a toggle button beside the facets. */
export interface ListingToggle {
  id: string;
  label: string;
  pressed: boolean;
  onPressedChange: (on: boolean) => void;
  icon?: Snippet;
}

export interface ListingToolbarProps {
  /** Drawn first, growing to fill the first line: the listing's `ListingSearch`. */
  search?: Snippet | undefined;
  /** Drawn after the facets and toggles, in the left group: a summary strip. */
  afterFacets?: Snippet | undefined;
  /** Drawn last among the filters, at the end of their line: a sort. */
  beforeControls?: Snippet | undefined;
  /**
   * Drawn at the end of the first line, before the field picker and view
   * toggle, and kept there when the filters wrap: a mode switch such as Live.
   */
  trailing?: Snippet | undefined;
  /** Drawn first, in the order the listing declares them. */
  facets?: readonly FacetDimension[];
  /** Omit both to draw no field picker, as `ListingControls` does. */
  fields?: ListField[] | undefined;
  fieldState?: VisibleFields | undefined;
  /** Omit for a listing with one way to draw itself: no view toggle is drawn. */
  view?: ListingView | undefined;
  /** Drawn after the facets, before `afterFacets`. */
  toggles?: readonly ListingToggle[];
  /** Whether the listing is narrowed. Defaults to any facet picked or toggle pressed. */
  filtersActive?: boolean | undefined;
  /** Replaces the default reset, for a listing whose filters live on the server. */
  onClearFilters?: (() => void) | undefined;
  class?: ClassValue | undefined;
}
