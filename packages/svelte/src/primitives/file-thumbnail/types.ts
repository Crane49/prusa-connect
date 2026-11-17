import type { LucideIcon } from '@lucide/svelte';
import type { ClassValue } from 'svelte/elements';

export interface FileThumbnailProps {
  /** Names the preview for a screen reader, and captions it. */
  name: string;
  /** Absent for a folder, and for a file this instance has no preview of. */
  thumbnailUrl?: string | undefined;
  glyph: LucideIcon;
  /**
   * Whether this row's preview is the one showing. The open state is the
   * caller's: a listing wants one open preview at a time, and only the list
   * knows which row that is.
   */
  open?: boolean;
  class?: ClassValue | undefined;
}
