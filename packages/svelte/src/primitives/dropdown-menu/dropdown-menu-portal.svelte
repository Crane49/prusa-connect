<!--
  Moves its children to another place in the document. `DropdownMenuContent`
  portals itself, so this part is for a caller that wants a different target.

  It renders no element, so it takes no `data-slot` and no class.
-->
<script lang="ts">
  import { DropdownMenu as DropdownMenuPrimitive } from 'bits-ui';

  import type { DropdownMenuPortalProps } from './types';

  let { ...rest }: DropdownMenuPortalProps = $props();
</script>

<DropdownMenuPrimitive.Portal {...rest} />
