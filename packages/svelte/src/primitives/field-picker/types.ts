import type { ClassValue } from 'svelte/elements';

import type { ControlSize } from '../control-trigger/types';

export interface ListField {
  /** Stable key, and what `visible` is asked about. */
  id: string;
  label: string;
  /** Identity and controls: ticked, and not pickable. */
  required?: boolean;
  /** Hidden until a reader asks for it. */
  hiddenByDefault?: boolean;
}

/**
 * Which fields a listing draws, and the two ways a reader changes it.
 *
 * The listing holds the set. What the defaults are is read off `fields`, so
 * the picker decides on its own whether anything has been changed and whether
 * to offer the way back.
 */
export interface VisibleFields {
  /** The ids the listing draws now. */
  visible: string[];
  onToggle: (id: string) => void;
  onReset: () => void;
}

export interface FieldPickerProps {
  fields: ListField[];
  state: VisibleFields;
  /** The trigger's accessible name and its tooltip. */
  label?: string | undefined;
  /**
   * The control scale, shared with the view toggle. `sm` by default, so the
   * two sit as one cluster.
   */
  size?: ControlSize;
  class?: ClassValue | undefined;
}
