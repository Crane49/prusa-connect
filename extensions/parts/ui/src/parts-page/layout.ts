// The split the catalogue view and its skeleton share: the folder tree beside
// the listing, side by side at the one width where both fit, stacked below it.
// The Storage screen's split, read against this page's own container.
//
// Every column of this module stacks with `grid`, never with a flex column,
// and no stack here takes the four-step gap. The build scopes a module's
// utilities to its slot, and a scoped rule outranks the host's rule of the
// same weight. A flex-column utility this module emitted would then also beat
// the library's own container variants on the detail header and the
// key-value list drawn inside this page, and stack them at every width; the
// four-step gap would do the same to the gap of every Card. Tailwind reads
// class names out of comments too, so this one names neither.

/**
 * The row both columns sit in, a grid at every width that gains its second
 * column at the wide one. It never switches display: every module's sheet is
 * scoped to any module slot, not to its own, so a plain grid utility from a
 * module loaded after this one would outrank a container variant switching
 * this row to another display, and keep it stacked.
 */
export const SPLIT_ROW =
  'grid @4xl/parts:grid-cols-[minmax(16rem,1fr)_2fr] @4xl/parts:items-stretch';

/**
 * The dropzone and the folder tree, a third of the width. Its rows start at
 * the top: the row is as tall as the listing beside it, and a grid left to
 * stretch would share that height out as gaps between the tree and the tags.
 */
export const SPLIT_LEADING =
  'grid content-start gap-y-6 border-b border-border pb-6 @4xl/parts:border-b-0 @4xl/parts:pb-0 @4xl/parts:pe-6';

/** The listing, with the rule between the two columns. */
export const SPLIT_TRAILING =
  'border-border pt-6 @4xl/parts:border-s @4xl/parts:pt-0 @4xl/parts:ps-6';

/** The grid the cards of the listing sit in. */
export const CARD_GRID =
  'grid grid-cols-2 gap-3 @2xl/parts:grid-cols-3 @5xl/parts:grid-cols-4 @7xl/parts:grid-cols-5';
