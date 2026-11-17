/**
 * Nozzle, heatbed and spool, paths verbatim from Prusa Connect's own
 * `public/img/icons/`. Inlined rather than loaded by URL the way
 * `printer-icon` loads its own: these are `fill="currentColor"` line art that
 * must take its container's colour, and an `<img>` cannot inherit
 * `currentColor`. That is why the frozen app tints the same files through
 * per-icon invert, sepia and hue-rotate filters instead. The partial
 * `opacity="0.6"` paths are the source files' own.
 *
 * A glyph keeps its source file's own `viewBox`, so the paths stay verbatim
 * and nobody has to re-fit one by hand.
 *
 * Callers set `aria-hidden` themselves. Whether the icon is decoration or the
 * label is theirs to decide.
 */
export { default as HeatbedIcon } from './heatbed-icon.svelte';
export { default as NozzleIcon } from './nozzle-icon.svelte';
export { default as SpoolIcon } from './spool-icon.svelte';
export type { TelemetryIconProps } from './types';
