<script lang="ts">
  import { AlertDialog as AlertDialogPrimitive } from 'bits-ui';

  import type { AlertDialogTriggerProps } from './types';

  let { ref = $bindable(null), ...rest }: AlertDialogTriggerProps = $props();
</script>

<AlertDialogPrimitive.Trigger
  bind:ref
  data-slot="alert-dialog-trigger"
  {...rest}
/>
