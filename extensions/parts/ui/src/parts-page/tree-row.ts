// The look of one row of the folder tree, shared by the top entry and every
// folder under it, so the two never differ by a pixel.

/**
 * The line a row sits on: the chevron and the link. It is also the drop
 * target, so files dragged over it mark the whole line.
 */
export const LINE =
  'flex min-w-0 items-center gap-0.5 rounded-sm transition-colors motion-reduce:transition-none data-dropping:bg-secondary data-dropping:text-secondary-foreground data-dropping:ring-2 data-dropping:ring-primary data-dropping:ring-inset';

/**
 * The link of one row: a glyph, the name, and how much it holds. It carries
 * no colour of its own: one of the two below is always set beside it, so the
 * two never compete for the same property.
 */
export const ROW =
  'flex min-h-8 min-w-0 flex-1 items-center gap-2 rounded-sm px-1.5 text-sm outline-none transition-colors focus-visible:ring-2 focus-visible:ring-ring motion-reduce:transition-none';

/** Every row but the folder on screen. */
export const ROW_IDLE = 'hover:bg-accent';

/**
 * The row of the folder on screen: a quiet ground and a heavier name, with
 * the glyph in the primary colour. The app's own navigation already draws the
 * page it is on in the primary colour, and a second block of it beside that
 * would compete with it.
 */
export const ROW_CURRENT = 'bg-accent font-medium text-foreground';

/** The glyph of the row of the folder on screen. */
export const GLYPH_CURRENT = 'size-4 shrink-0 text-primary';

/** How many parts a row holds. */
export const COUNT =
  'min-w-5 rounded-xs px-1 text-center text-xs text-muted-foreground tabular-nums';

/**
 * The folders inside a row, hung off a guide that runs down from the middle
 * of the row's chevron, so the depth is read from the lines and not counted.
 */
export const CHILDREN = 'ms-3.5 grid border-s border-border ps-1.5';
