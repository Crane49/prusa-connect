/**
 * The materials glyph: three hexagons, the top one in the primary colour, as
 * the materials entry of the Prusa Connect mobile app draws them.
 *
 * It renders through lucide's own `Icon`, so it takes the props a lucide icon
 * takes (`class`, `size`, `color`, `aria-*`) and fits wherever a `LucideIcon`
 * is expected. The stroke is set to read as lucide's 2 on the 24 grid, so
 * `strokeWidth` does not change it.
 */
export { default as MaterialsGlyphIcon } from './materials-glyph-icon.svelte';
