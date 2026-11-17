<script lang="ts">
  import { ToggleGroup as ToggleGroupPrimitive } from 'bits-ui';

  import { base, sizes, variants } from '../toggle/variants';
  import { getGroupLook } from './context';
  import type { ToggleGroupItemProps } from './types';

  let {
    ref = $bindable(null),
    class: className,
    variant,
    size,
    ...rest
  }: ToggleGroupItemProps = $props();

  const groupLook = getGroupLook();
  const look = $derived(groupLook?.());
  const itemVariant = $derived(variant ?? look?.variant ?? 'default');
  const itemSize = $derived(size ?? look?.size ?? 'md');
</script>

<ToggleGroupPrimitive.Item
  bind:ref
  data-slot="toggle-group-item"
  data-variant={itemVariant}
  data-size={itemSize}
  class={[
    base,
    variants[itemVariant],
    sizes[itemSize],
    // The items sit edge to edge, so the group's outer corners are the first
    // and last item's own. Under `outline` each shared edge would be drawn
    // twice, and `border-l-0` with `first:border-l` collapses it to one
    // hairline.
    'w-auto shrink-0 rounded-none px-3 first:rounded-l-sm last:rounded-r-sm focus:z-10 focus-visible:z-10 data-[variant=outline]:border-l-0 data-[variant=outline]:first:border-l',
    className,
  ]}
  {...rest}
/>
