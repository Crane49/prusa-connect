import type { ClassValue, HTMLInputAttributes } from 'svelte/elements';

import type { ControlSize } from '../control-trigger/types';

export interface InputProps extends Omit<
  HTMLInputAttributes,
  'class' | 'size'
> {
  /**
   * The shared control scale: `sm` 28px, `md` 36px, `lg` 44px. It shadows the
   * native `size` attribute, which counts characters and no caller here wants.
   */
  size?: ControlSize;
  class?: ClassValue | undefined;
}
