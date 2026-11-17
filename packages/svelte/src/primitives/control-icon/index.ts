/**
 * The control glyphs, path for path from Prusa Connect's own icon set: the
 * arrows a jog pad is pressed with, Home, Disable steppers, Reset, Mesh bed
 * leveling, and the badges the Temperatures, Speed, Z and Chamber sections
 * carry. One product, one vocabulary of shapes. A farm operator moving
 * between Connect and this instance should not have to learn a second set of
 * pictures for the same five commands.
 *
 * Every glyph is inlined rather than loaded by URL, and every
 * `var(--icon-primary)`, `--icon-brand` and `--icon-secondary` is rewritten to
 * `currentColor`, exactly as `telemetry-icon` carries nozzle, heatbed and
 * spool. An `<img>` cannot inherit `currentColor`, and these have to take the
 * tint of the button or heading they sit in. Where the source is two-tone, the
 * secondary tone keeps its distinction as `opacity="0.6"`.
 *
 * A glyph keeps its source file's own `viewBox`, so the paths stay verbatim
 * and nobody has to re-fit one by hand.
 *
 * Callers set `aria-hidden` themselves. Whether the glyph is decoration or the
 * label is theirs to decide.
 */
export { default as ArrowDownIcon } from './arrow-down-icon.svelte';
export { default as ArrowLeftIcon } from './arrow-left-icon.svelte';
export { default as ArrowRightIcon } from './arrow-right-icon.svelte';
export { default as ArrowUpIcon } from './arrow-up-icon.svelte';
export { default as ChamberIcon } from './chamber-icon.svelte';
export { default as DisableSteppersIcon } from './disable-steppers-icon.svelte';
export { default as HomeIcon } from './home-icon.svelte';
export { default as MeshBedLevelingIcon } from './mesh-bed-leveling-icon.svelte';
export { default as RestartIcon } from './restart-icon.svelte';
export { default as SpeedIcon } from './speed-icon.svelte';
export { default as TemperatureIcon } from './temperature-icon.svelte';
export { default as ZAxisIcon } from './z-axis-icon.svelte';
export type { ControlIconProps } from './types';
