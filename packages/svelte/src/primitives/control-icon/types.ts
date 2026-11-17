import type { SVGAttributes } from 'svelte/elements';

/**
 * A glyph takes whatever an `<svg>` takes, and every attribute a caller sets
 * lands on the element.
 *
 * `class` adds to the `size-4 shrink-0` the glyph already carries. Nothing
 * merges the two, so a caller that passes `size-8` wins on stylesheet order
 * alone.
 */
export type ControlIconProps = SVGAttributes<SVGSVGElement>;
