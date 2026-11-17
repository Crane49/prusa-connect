<script lang="ts">
  import { Popover as PopoverPrimitive } from 'bits-ui';

  import type { PopoverTriggerProps } from './types';

  let { ref = $bindable(null), ...rest }: PopoverTriggerProps = $props();
</script>

<PopoverPrimitive.Trigger bind:ref data-slot="popover-trigger" {...rest} />
