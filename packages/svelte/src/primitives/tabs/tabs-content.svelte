<script lang="ts">
  import { Tabs as TabsPrimitive } from 'bits-ui';

  import type { TabsContentProps } from './types';

  let {
    ref = $bindable(null),
    class: className,
    ...rest
  }: TabsContentProps = $props();
</script>

<TabsPrimitive.Content
  bind:ref
  data-slot="tabs-content"
  class={['flex-1 outline-none', className]}
  {...rest}
/>
