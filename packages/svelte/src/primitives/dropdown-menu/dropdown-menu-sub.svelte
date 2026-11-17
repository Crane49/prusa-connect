<!--
  A submenu. bits-ui carries its own open state, and the parent menu stays open
  while it is up.

  It renders no element, so it takes no `data-slot` and no class.
-->
<script lang="ts">
  import { DropdownMenu as DropdownMenuPrimitive } from 'bits-ui';

  import type { DropdownMenuSubProps } from './types';

  let { open = $bindable(false), ...rest }: DropdownMenuSubProps = $props();
</script>

<DropdownMenuPrimitive.Sub bind:open {...rest} />
