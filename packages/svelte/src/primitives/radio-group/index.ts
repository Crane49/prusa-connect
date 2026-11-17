import RadioGroupRoot from './radio-group.svelte';

/**
 * Focus lands on the checked item and never on this root, so a `Field` refuses
 * to wrap the group rather than write an id and a description onto a node no
 * screen reader reaches. The name travels as a value, because a minified build
 * does not keep the function's own name.
 */
export const RadioGroup = Object.assign(RadioGroupRoot, {
  rovingTabindexRoot: 'RadioGroup',
});
export { default as RadioGroupItem } from './radio-group-item.svelte';
export type { RadioGroupItemProps, RadioGroupProps } from './types';
