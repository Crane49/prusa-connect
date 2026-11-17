import type { Component, Snippet } from 'svelte';
import type { ClassValue } from 'svelte/elements';

import type { ButtonProps } from '../button/types';

/**
 * The one act a screen leads with. A value rather than a snippet because what
 * repeats between screens is the position and the weight, not the button.
 */
export interface PageAction {
  label: string;
  icon?: Component;
  onclick: () => void;
  variant?: ButtonProps['variant'];
}

export interface PageHeaderProps {
  title: string;
  action?: PageAction | undefined;
  /** Anything richer than one button. Drawn in place of `action`. */
  actions?: Snippet;
  class?: ClassValue | undefined;
}
