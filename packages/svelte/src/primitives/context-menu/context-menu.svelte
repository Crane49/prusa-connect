<!--
  A menu a person opens with the right mouse button or a long press. bits-ui
  carries the open state, the pointer position the surface is anchored to, the
  roving focus and the escape key.

  The root renders no element, so it takes no `data-slot` and no class. Every
  class is on the parts below it.

  React's root takes a `modal` prop and defaults it to false, so that Radix
  does not mark the page `aria-hidden` while the trigger stays focusable.
  bits-ui declares no such prop and hides nothing from a screen reader, so the
  problem that default answers does not arise here. The page scroll is the one
  part of modality bits-ui does carry, and `ContextMenuContent` turns it off.
-->
<script lang="ts">
  import { ContextMenu as ContextMenuPrimitive } from 'bits-ui';

  import type { ContextMenuProps } from './types';

  let { open = $bindable(false), ...rest }: ContextMenuProps = $props();
</script>

<ContextMenuPrimitive.Root bind:open {...rest} />
