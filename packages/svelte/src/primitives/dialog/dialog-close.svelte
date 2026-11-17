<script lang="ts">
  import { Dialog as DialogPrimitive } from 'bits-ui';

  import type { DialogCloseProps } from './types';

  let { ref = $bindable(null), ...rest }: DialogCloseProps = $props();
</script>

<DialogPrimitive.Close bind:ref data-slot="dialog-close" {...rest} />
