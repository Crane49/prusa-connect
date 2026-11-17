<!--
  One tooltip. bits-ui carries the open state, the hover and focus timing and
  the escape key.

  The root renders no element, so it takes no `data-slot` and no class.
-->
<script lang="ts">
  import { Tooltip as TooltipPrimitive } from 'bits-ui';

  import type { TooltipProps } from './types';

  let { open = $bindable(false), ...rest }: TooltipProps = $props();
</script>

<TooltipPrimitive.Root bind:open {...rest} />
