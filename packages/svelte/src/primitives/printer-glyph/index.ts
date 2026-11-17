/**
 * The generic printer glyph: the printer of the bottom navigation in the
 * Prusa Connect mobile app, so both apps draw a printer the same way.
 *
 * It renders through lucide's own `Icon`, so it takes the props a lucide icon
 * takes (`class`, `size`, `color`, `aria-*`) and fits wherever a `LucideIcon`
 * is expected. The drawing is a filled outline, so `strokeWidth` does not
 * change it.
 *
 * `PrinterIcon` is a different thing: the drawing of one printer model.
 */
export { default as PrinterGlyphIcon } from './printer-glyph-icon.svelte';
