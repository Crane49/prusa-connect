import type { LucideIcon } from '@lucide/svelte';
import type { Snippet } from 'svelte';
import type { ClassValue, HTMLAttributes } from 'svelte/elements';

/** Which heading tag the message is drawn as. */
export type EmptyStateHeadingLevel = 'h1' | 'h2' | 'h3' | 'h4' | 'h5' | 'h6';

interface EmptyStateBaseProps extends Omit<
  HTMLAttributes<HTMLDivElement>,
  'class' | 'children'
> {
  /** Drawn above the heading, at the size the design system sets. */
  icon?: LucideIcon | undefined;
  heading: string;
  description?: string | undefined;
  /** What a person can do about it, usually one button. */
  action?: Snippet;
  class?: ClassValue | undefined;
}

/**
 * A full message has no default heading level: the component cannot know its
 * place in the page's heading order, and a level that skips one is a fault a
 * caller has to decide. `sm` fits a card: it fills its column, with a smaller
 * glyph and type, and its heading is an `h3` unless the caller says otherwise.
 */
export type EmptyStateProps = EmptyStateBaseProps &
  (
    | { size?: 'md'; headingLevel: EmptyStateHeadingLevel }
    | { size: 'sm'; headingLevel?: EmptyStateHeadingLevel }
  );
