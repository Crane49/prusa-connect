import type { ClassValue, HTMLAttributes } from 'svelte/elements';

/**
 * What the last copy did. It picks the glyph on the button and the sentence
 * under the block.
 */
export type CopyStatus = 'idle' | 'pending' | 'copied' | 'failed';

export interface CodeBlockProps extends Omit<
  HTMLAttributes<HTMLDivElement>,
  'class' | 'children'
> {
  /**
   * Names what the text is. Without one the copy button stands alone at the
   * end of the row above the block.
   */
  label?: string;
  /** The text itself. */
  value: string;
  /**
   * How tall the block is, as a definite `h-*` class.
   *
   * A cap does not work here. The scroll area's viewport is the full height of
   * its root, and a percentage resolves against a definite height and not
   * against a capped one, so `max-h-96` draws the block at its full length and
   * the page scrolls instead of the block.
   */
  heightClass?: string;
  /**
   * A file name. When set, a download button beside the copy button saves the
   * text under that name.
   */
  download?: string | undefined;
  class?: ClassValue | undefined;
}
