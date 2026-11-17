import type { Snippet } from 'svelte';
import type { ClassValue } from 'svelte/elements';

import type { ControlSize } from '../control-trigger/types';

export interface FacetPopoverProps {
  label: string;
  /** The control scale, shared with the select, the input and the toggle. */
  size?: ControlSize | undefined;
  /** What is picked, already shortened. Absent means nothing is picked. */
  summary?: string | undefined;
  /** How many more values there are than the summary lists. */
  count?: number | undefined;
  /** Forces the on state when the value is carried by something else, a swatch. */
  active?: boolean | undefined;
  /** CSS colours, drawn in the trigger for a facet words cannot summarise. */
  swatches?: string[] | undefined;
  /** How wide the panel is, in pixels. */
  width: number;
  class?: ClassValue | undefined;
  children: Snippet;
}
