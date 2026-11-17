<!--
  The button that goes ahead with the thing the panel asks about.

  bits-ui puts no click handler on this part, so pressing it runs the caller's
  `onclick` and leaves the panel open. The caller shuts it, which is what lets
  a slow action keep the panel up while it runs.

  The element is drawn through the `child` snippet so it is the product's
  Button. bits-ui merges nothing for a snippet, so `props` carries every
  attribute and every handler.
-->
<script lang="ts">
  import { AlertDialog as AlertDialogPrimitive } from 'bits-ui';

  import Button from '../button/button.svelte';
  import type { AlertDialogActionProps } from './types';

  let {
    ref = $bindable(null),
    variant = 'default',
    size = 'md',
    children,
    ...rest
  }: AlertDialogActionProps = $props();
</script>

<AlertDialogPrimitive.Action bind:ref data-slot="alert-dialog-action" {...rest}>
  {#snippet child({ props })}
    <Button {...props} {variant} {size}>{@render children?.()}</Button>
  {/snippet}
</AlertDialogPrimitive.Action>
