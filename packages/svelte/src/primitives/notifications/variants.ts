import type { NotificationTone } from '../../domain';

// Static strings chosen by key (svelte-5.md). Tailwind reads this file, so a
// utility written here is emitted; one assembled at run time is not.

// The chip behind the glyph: the tone's own hue at a tenth, under its step-11
// text tone. A fill rather than a bare glyph, so the four tones read as one
// column of markers down the leading edge of the list. It is also what a row
// with no artwork keeps in place of the machine or the plate.
export const TONE_CHIP_BASE =
  'grid size-10 shrink-0 place-items-center rounded-sm';

export const TONE_CHIP: Record<NotificationTone, string> = {
  critical: 'bg-destructive/10 text-destructive-text',
  attention: 'bg-warning/15 text-warning-text',
  success: 'bg-success/15 text-success-text',
  info: 'bg-info/15 text-info-text',
};

// The same glyph over the corner of a row's artwork, ringed in the panel's own
// surface so it stays legible over a plate of any colour.
export const TONE_CORNER_BASE =
  'absolute -right-1 -bottom-1 grid size-4.5 place-items-center rounded-full bg-popover ring-2 ring-popover';

export const TONE_CORNER: Record<NotificationTone, string> = {
  critical: 'text-destructive-text',
  attention: 'text-warning-text',
  success: 'text-success-text',
  info: 'text-info-text',
};

// Unread is one dot beside the timestamp, in the tone's own colour. A rule
// down the whole leading edge, under a coloured tone word, a tinted corner
// glyph and a filled action button, made a list of four rows read as a string
// of fairy lights. A dot says the same thing in 8px, and the title's weight
// says it again for anyone who cannot see the colour.
export const TONE_DOT_BASE = 'size-2 shrink-0 rounded-full';

export const TONE_DOT: Record<NotificationTone, string> = {
  critical: 'bg-destructive',
  attention: 'bg-warning',
  success: 'bg-success',
  info: 'bg-info',
};

/**
 * The word beside the glyph, so the tone survives greyscale, a screenshot and
 * a reader who does not see the tint.
 */
export const TONE_LABEL: Record<NotificationTone, string> = {
  critical: 'primitives.notifications.tone.critical',
  attention: 'primitives.notifications.tone.attention',
  success: 'primitives.notifications.tone.success',
  info: 'primitives.notifications.tone.info',
};
