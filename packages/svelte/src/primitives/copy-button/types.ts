import type { ButtonIconSize } from '../button/types';

/**
 * Where a copy has got to. It picks the glyph, it disables the button while
 * the write is in flight, and it names the sentence the status region reads.
 */
export type CopyStatus = 'idle' | 'pending' | 'copied' | 'failed';

/** A square button on the control scale. A copy button holds one glyph. */
export type CopyButtonSize = ButtonIconSize;

export interface CopyButtonProps {
  /**
   * The text the press puts on the clipboard. A function is called at the
   * press, for a text that changes after the button was drawn.
   */
  value: string | (() => string);
  /**
   * What is being copied, for the button's own name where more than one sits
   * on a screen: "Copy serial number". It defaults to the plain word.
   */
  label?: string;
  /**
   * Called on every step of the machine, for a caller that draws the outcome
   * itself. `CopyField` writes the sentence under its input from this.
   */
  onStatusChange?: (status: CopyStatus) => void;
  /**
   * Whether this button carries the live region. Set it to `false` in a
   * caller that renders the message itself: two regions announce the same
   * copy twice.
   */
  announce?: boolean;
  /**
   * The id of a caller's own status element, so the outcome is read as this
   * button's accessible description.
   */
  describedBy?: string;
  size?: CopyButtonSize;
}
