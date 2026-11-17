<!--
  The control the tooltip names. bits-ui points its `aria-describedby` at the
  surface while the tooltip is open, which is what reads the text out.
-->
<script lang="ts">
  import { Tooltip as TooltipPrimitive } from 'bits-ui';

  import type { TooltipTriggerProps } from './types';

  let { ref = $bindable(null), ...rest }: TooltipTriggerProps = $props();
</script>

<TooltipPrimitive.Trigger bind:ref data-slot="tooltip-trigger" {...rest} />
