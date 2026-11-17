<!--
  The group one radio item at a time is picked from. bits-ui holds the chosen
  value and marks the matching item.

  It draws nothing, so it takes no class.
-->
<script lang="ts">
  import { ContextMenu as ContextMenuPrimitive } from 'bits-ui';

  import type { ContextMenuRadioGroupProps } from './types';

  let {
    ref = $bindable(null),
    value = $bindable(''),
    ...rest
  }: ContextMenuRadioGroupProps = $props();
</script>

<ContextMenuPrimitive.RadioGroup
  bind:ref
  bind:value
  data-slot="context-menu-radio-group"
  {...rest}
/>
