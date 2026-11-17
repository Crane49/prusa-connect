<!--
  The line between two runs of items.

  bits-ui gives the element `role="group"`, where Radix gives it
  `role="separator"`. The line is decoration either way, and the role is
  bits-ui's to set.
-->
<script lang="ts">
  import { DropdownMenu as DropdownMenuPrimitive } from 'bits-ui';

  import type { DropdownMenuSeparatorProps } from './types';

  let {
    ref = $bindable(null),
    class: className,
    ...rest
  }: DropdownMenuSeparatorProps = $props();
</script>

<DropdownMenuPrimitive.Separator
  bind:ref
  data-slot="dropdown-menu-separator"
  class={['-mx-1 my-1 h-px bg-border', className]}
  {...rest}
/>
