<!--
  What answering the question does. bits-ui reports its `id` to the content,
  which points its `aria-describedby` here.
-->
<script lang="ts">
  import { AlertDialog as AlertDialogPrimitive } from 'bits-ui';

  import type { AlertDialogDescriptionProps } from './types';

  let {
    ref = $bindable(null),
    class: className,
    ...rest
  }: AlertDialogDescriptionProps = $props();
</script>

<AlertDialogPrimitive.Description
  bind:ref
  data-slot="alert-dialog-description"
  class={['text-sm wrap-anywhere text-muted-foreground', className]}
  {...rest}
/>
