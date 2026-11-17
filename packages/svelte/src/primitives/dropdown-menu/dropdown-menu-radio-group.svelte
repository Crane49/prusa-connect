<!--
  The group one radio item at a time is picked from. bits-ui holds the chosen
  value and marks the matching item.

  It draws nothing, so it takes no class.
-->
<script lang="ts">
  import { DropdownMenu as DropdownMenuPrimitive } from 'bits-ui';

  import type { DropdownMenuRadioGroupProps } from './types';

  let {
    ref = $bindable(null),
    value = $bindable(''),
    ...rest
  }: DropdownMenuRadioGroupProps = $props();
</script>

<DropdownMenuPrimitive.RadioGroup
  bind:ref
  bind:value
  data-slot="dropdown-menu-radio-group"
  {...rest}
/>
