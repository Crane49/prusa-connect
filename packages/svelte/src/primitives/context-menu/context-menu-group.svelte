<!--
  A run of items that belong together. bits-ui gives it `role="group"`.

  It draws nothing, so it takes no class.
-->
<script lang="ts">
  import { ContextMenu as ContextMenuPrimitive } from 'bits-ui';

  import type { ContextMenuGroupProps } from './types';

  let { ref = $bindable(null), ...rest }: ContextMenuGroupProps = $props();
</script>

<ContextMenuPrimitive.Group bind:ref data-slot="context-menu-group" {...rest} />
