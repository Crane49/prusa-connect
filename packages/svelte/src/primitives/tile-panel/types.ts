import type { Snippet } from 'svelte';
import type { ClassValue } from 'svelte/elements';

export interface TilePanelProps {
  /** The heading a person wrote; blank means `fallback` is shown. */
  title: string;
  /** The heading the tile's kind gives it. */
  fallback?: string | undefined;
  subline?: string | undefined;
  /** Drawn at the end of the heading row. */
  trailing?: Snippet | undefined;
  children: Snippet;
  class?: ClassValue | undefined;
}
