<script lang="ts">
  import { Tabs as TabsPrimitive } from 'bits-ui';

  import type { TabsListProps } from './types';

  let {
    ref = $bindable(null),
    class: className,
    ...rest
  }: TabsListProps = $props();
</script>

<!-- A rule under the whole row, never a pill. The active trigger breaks it
     with its own coloured underline. -->
<TabsPrimitive.List
  bind:ref
  data-slot="tabs-list"
  class={[
    'inline-flex h-9 w-fit items-center justify-center gap-4 border-b border-border',
    className,
  ]}
  {...rest}
/>
