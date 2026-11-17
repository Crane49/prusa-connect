import type { Snippet } from 'svelte';
import type { ClassValue, HTMLAttributes } from 'svelte/elements';

/**
 * The looks a badge takes. The first seven are the general ones. The six
 * `state*` names are printer state, and a caller picks one from the printer's
 * own state rather than from the colour it wants.
 */
export type BadgeVariant =
  | 'default'
  | 'secondary'
  | 'outline'
  | 'success'
  | 'warning'
  | 'info'
  | 'destructive'
  | 'stateActive'
  | 'stateDone'
  | 'stateIdle'
  | 'stateAttention'
  | 'stateFault'
  | 'stateQuiet';

export type BadgeSize = 'sm' | 'md' | 'lg';

export interface BadgeProps extends Omit<
  HTMLAttributes<HTMLSpanElement>,
  'class' | 'children'
> {
  variant?: BadgeVariant;
  /**
   * `sm` is `py-0.5` and `md` is `py-1`. `lg` is a pill that holds a picture
   * at its start: roomier, with larger text.
   */
  size?: BadgeSize;
  /**
   * Monospace, uppercase and tracked. On by default for the `state*` variants
   * and off for the rest.
   */
  mono?: boolean;
  /**
   * Monospace digits that keep their width, for a reading such as a speed.
   * Unlike `mono` it leaves the case alone.
   */
  numeric?: boolean;
  class?: ClassValue | undefined;
  children?: Snippet;
  /**
   * Draw the badge as another element, usually a link to what it labels. The
   * snippet receives the attributes the `<span>` would have had and puts them
   * where it wants them. Nothing is merged for it: an attribute the snippet
   * does not spread is lost.
   */
  child?: Snippet<[{ props: Record<string, unknown> }]>;
}
