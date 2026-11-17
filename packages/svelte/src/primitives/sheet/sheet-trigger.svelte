<script lang="ts">
  import { Dialog as SheetPrimitive } from 'bits-ui';

  import type { SheetTriggerProps } from './types';

  let { ref = $bindable(null), ...rest }: SheetTriggerProps = $props();
</script>

<SheetPrimitive.Trigger bind:ref data-slot="sheet-trigger" {...rest} />
