/**
 * The picked state, over the shared control box: a filled control, the same
 * fill the toggle gives its on state. Only this part is the facet's own.
 * Everything else about the box is what a select's trigger wears, because a
 * reader meets both in one row.
 *
 * The floor and the cap make a filter row read as a set of equal boxes rather
 * than a ragged line of assorted widths, and keep one long value from taking
 * the whole row.
 */
export const FACET_TRIGGER =
  'max-w-66 min-w-30 data-on:border-transparent data-on:bg-selected data-on:text-selected-foreground data-on:hover:bg-selected-hovered data-on:[&_svg]:text-selected-foreground';

/**
 * The same picked fill without the width: a listing toolbar's yes-or-no
 * filter stands in the facets' row and switches on the way a facet does.
 */
export const CONTROL_ON =
  'data-on:border-transparent data-on:bg-selected data-on:text-selected-foreground data-on:hover:bg-selected-hovered data-on:[&_svg]:text-selected-foreground';
