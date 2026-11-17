<script lang="ts">
  import { Dialog as DialogPrimitive } from 'bits-ui';

  import type { DialogDescriptionProps } from './types';

  let {
    ref = $bindable(null),
    class: className,
    ...rest
  }: DialogDescriptionProps = $props();
</script>

<DialogPrimitive.Description
  bind:ref
  data-slot="dialog-description"
  class={['text-sm break-words text-muted-foreground', className]}
  {...rest}
/>
