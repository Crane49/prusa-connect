<!--
  A run of items that belong together. bits-ui gives it `role="group"`.

  It draws nothing, so it takes no class.
-->
<script lang="ts">
  import { DropdownMenu as DropdownMenuPrimitive } from 'bits-ui';

  import type { DropdownMenuGroupProps } from './types';

  let { ref = $bindable(null), ...rest }: DropdownMenuGroupProps = $props();
</script>

<DropdownMenuPrimitive.Group
  bind:ref
  data-slot="dropdown-menu-group"
  {...rest}
/>
