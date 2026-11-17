<script lang="ts">
  import { Dialog as DialogPrimitive } from 'bits-ui';

  import type { DialogTriggerProps } from './types';

  let { ref = $bindable(null), ...rest }: DialogTriggerProps = $props();
</script>

<DialogPrimitive.Trigger bind:ref data-slot="dialog-trigger" {...rest} />
