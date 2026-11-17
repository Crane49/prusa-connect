<!--
  Moves its children to another place in the document. `ContextMenuContent`
  portals itself, so this part is for a caller that wants a different target.

  It renders no element, so it takes no `data-slot` and no class.
-->
<script lang="ts">
  import { ContextMenu as ContextMenuPrimitive } from 'bits-ui';

  import type { ContextMenuPortalProps } from './types';

  let { ...rest }: ContextMenuPortalProps = $props();
</script>

<ContextMenuPrimitive.Portal {...rest} />
