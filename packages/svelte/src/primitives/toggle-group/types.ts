import type { ToggleGroup as ToggleGroupPrimitive } from 'bits-ui';

import type { ToggleSize, ToggleVariant } from '../toggle/types';

export type ToggleGroupProps = ToggleGroupPrimitive.RootProps & {
  variant?: ToggleVariant;
  size?: ToggleSize;
};

export type ToggleGroupItemProps = ToggleGroupPrimitive.ItemProps & {
  variant?: ToggleVariant;
  size?: ToggleSize;
};
