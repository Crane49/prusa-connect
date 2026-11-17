import type { Snippet } from 'svelte';
import type { ClassValue } from 'svelte/elements';

import type { ListField, VisibleFields } from '../field-picker/types';
import type { ViewModeOption } from '../view-mode/types';

/** What the listing is drawn as, and how the reader changes it. */
export interface ListingView {
  value: string;
  onValueChange: (next: string) => void;
  options: ViewModeOption[];
  /** The group's own accessible name, where `Display mode` is not specific enough. */
  label?: string | undefined;
}

export interface ListingControlsProps {
  /**
   * Omit both to render no field picker. A listing whose rows carry two facts
   * has nothing to put away.
   */
  fields?: ListField[] | undefined;
  fieldState?: VisibleFields | undefined;
  view: ListingView;
  /** Anything the screen keeps beside the pair: a sort control, a count. */
  children?: Snippet;
  class?: ClassValue | undefined;
}
