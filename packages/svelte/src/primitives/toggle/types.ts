import type { Toggle as TogglePrimitive } from 'bits-ui';

/** The resting look: no edge at all, or a hairline border around it. */
export type ToggleVariant = 'default' | 'outline';

/** The three heights, the same set Button offers. */
export type ToggleSize = 'sm' | 'md' | 'lg';

export type ToggleProps = TogglePrimitive.RootProps & {
  variant?: ToggleVariant;
  size?: ToggleSize;
};
