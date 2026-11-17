<!--
  The button that answers no. bits-ui shuts the panel when it is pressed.

  It is the quiet fill, so the action beside it is the one a person reads
  first. The element is drawn through the `child` snippet so it is the
  product's Button.
-->
<script lang="ts">
  import { AlertDialog as AlertDialogPrimitive } from 'bits-ui';

  import Button from '../button/button.svelte';
  import type { AlertDialogCancelProps } from './types';

  let {
    ref = $bindable(null),
    variant = 'secondary',
    size = 'md',
    children,
    ...rest
  }: AlertDialogCancelProps = $props();
</script>

<AlertDialogPrimitive.Cancel bind:ref data-slot="alert-dialog-cancel" {...rest}>
  {#snippet child({ props })}
    <Button {...props} {variant} {size}>{@render children?.()}</Button>
  {/snippet}
</AlertDialogPrimitive.Cancel>
