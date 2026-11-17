import type { AriaAttributes, ClassValue } from 'svelte/elements';

import type { ControlSize } from '../control-trigger/types';

export interface ComboboxItem {
  value: string;
  label: string;
}

export interface ComboboxProps {
  items: ComboboxItem[];
  /** The picked item's value, none while undefined. */
  value: string | undefined;
  onPick: (value: string | undefined) => void;
  /** The field's accessible name, and the list's. */
  label: string;
  placeholder?: string | undefined;
  size?: ControlSize;
  /**
   * Turns typed text no item is named into a Create row, and Enter on that
   * text into a call here. The caller makes the item and passes its value
   * back through `value`.
   */
  onCreate?: ((text: string) => void | Promise<void>) | undefined;
  /** The Create row's text for what is typed; the typed text itself when unset. */
  createLabel?: ((text: string) => string) | undefined;
  /** A row that clears the pick, shown while something is picked and nothing typed. */
  noneLabel?: string | undefined;
  /** Written on the `<input>`, so a `Field`'s label points at it. */
  id?: string | null | undefined;
  'aria-describedby'?: string | null | undefined;
  'aria-invalid'?: AriaAttributes['aria-invalid'];
  /** Written on the `<input>`, as `Input` and `Select` take theirs. */
  class?: ClassValue | undefined;
}
