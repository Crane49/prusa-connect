<!--
  A menu a person opens from a control. bits-ui carries the open state, the
  anchoring to the trigger, the roving focus, the typeahead and the escape key.

  The root renders no element, so it takes no `data-slot` and no class. Every
  class is on the parts below it.

  React's root takes a `modal` prop and sets it to false, because under modal
  Radix marks the trigger's ancestor `aria-hidden` and leaves the trigger
  clickable, so a pointer reaches a control assistive tech cannot see. bits-ui
  declares no `modal` prop and hides nothing, so that problem does not arise
  here. What modal also does, locking the page scroll, is a content prop in
  bits-ui and `DropdownMenuContent` turns it off.
-->
<script lang="ts">
  import { DropdownMenu as DropdownMenuPrimitive } from 'bits-ui';

  import type { DropdownMenuProps } from './types';

  let { open = $bindable(false), ...rest }: DropdownMenuProps = $props();
</script>

<DropdownMenuPrimitive.Root bind:open {...rest} />
