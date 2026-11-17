import type { Slider as SliderPrimitive } from 'bits-ui';

/**
 * One thumb or several. `type` is required: the headless layer decides the
 * shape of `value` from it, and will not read a lone number as a one thumb
 * slider on its own.
 */
export type SliderProps = SliderPrimitive.RootProps;
