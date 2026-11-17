<!--
  The heading of one group of options. bits-ui calls the part `GroupHeading`
  and names the group by it, which is the same element React calls the label.
-->
<script lang="ts">
  import { Select as SelectPrimitive } from 'bits-ui';

  import type { SelectLabelProps } from './types';

  let {
    ref = $bindable(null),
    class: className,
    ...rest
  }: SelectLabelProps = $props();
</script>

<SelectPrimitive.GroupHeading
  bind:ref
  data-slot="select-label"
  class={['px-2 py-1.5 text-xs font-semibold text-muted-foreground', className]}
  {...rest}
/>
