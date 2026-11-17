import ToggleGroupRoot from './toggle-group.svelte';

/**
 * Focus lands on the pressed item and never on this root, so a `Field` refuses
 * to wrap the group rather than write an id and a description onto a node no
 * screen reader reaches. The name travels as a value, because a minified build
 * does not keep the function's own name.
 */
export const ToggleGroup = Object.assign(ToggleGroupRoot, {
  rovingTabindexRoot: 'ToggleGroup',
});
export { default as ToggleGroupItem } from './toggle-group-item.svelte';
export type { ToggleGroupItemProps, ToggleGroupProps } from './types';
