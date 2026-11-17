// 56px is the smaller of the two sizes measured against a real render, 56 and
// 64, in both themes. At 40px a thumbnail reads as a colour blob; at 56px the
// plate's own layout, rows of parts and their rough shapes, is legible enough
// to tell one print's preview from another's, and 64px adds no legibility to
// earn the extra row height.
export const THUMBNAIL_BOX = 'size-14';

export const THUMBNAIL_GLYPH = 'size-6';

export const THUMBNAIL_BASE =
  'flex shrink-0 items-center justify-center overflow-hidden rounded-sm bg-surface';

/**
 * The box height plus a row's own vertical padding. The box is the tallest
 * thing in a row that carries one, so 72px is that row's real floor rather
 * than the shared one a data row defaults to.
 */
export const THUMBNAIL_ROW_MIN_HEIGHT = 'min-h-18';
