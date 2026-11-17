import type { LucideIcon } from '@lucide/svelte';
import type { Snippet } from 'svelte';
import type { HTMLButtonAttributes } from 'svelte/elements';

/** `page` reads as the page's title, `section` as a smaller heading. */
export type TitleButtonLevel = 'page' | 'section';

export interface TitleButtonProps extends Omit<
  HTMLButtonAttributes,
  'children' | 'aria-label'
> {
  level?: TitleButtonLevel | undefined;
  /** The glyph after the name that says what the button does. */
  icon: LucideIcon;
  /** The accessible name; it contains the visible name and says what the button does. */
  label: string;
  /** The name shown; it truncates when the row is narrow. */
  children: Snippet;
}
