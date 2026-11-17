<!--
  A submenu. bits-ui carries its own open state, and the parent menu stays open
  while it is up.

  It renders no element, so it takes no `data-slot` and no class.
-->
<script lang="ts">
  import { ContextMenu as ContextMenuPrimitive } from 'bits-ui';

  import type { ContextMenuSubProps } from './types';

  let { open = $bindable(false), ...rest }: ContextMenuSubProps = $props();
</script>

<ContextMenuPrimitive.Sub bind:open {...rest} />
