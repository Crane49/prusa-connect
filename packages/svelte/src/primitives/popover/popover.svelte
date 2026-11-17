<!--
  A surface anchored to the control that opened it. bits-ui carries the open
  state, the outside press, the escape key and the focus return.

  The root renders no element, so it takes no `data-slot` and no class. Every
  class is on the content.
-->
<script lang="ts">
  import { Popover as PopoverPrimitive } from 'bits-ui';

  import type { PopoverProps } from './types';

  let { open = $bindable(false), ...rest }: PopoverProps = $props();
</script>

<PopoverPrimitive.Root bind:open {...rest} />
