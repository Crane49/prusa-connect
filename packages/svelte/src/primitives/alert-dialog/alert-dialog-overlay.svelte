<!--
  The layer that dims the page behind the panel. Pressing it does nothing: an
  alert dialog is answered, not dismissed, and bits-ui ignores an interaction
  outside the content.
-->
<script lang="ts">
  import { AlertDialog as AlertDialogPrimitive } from 'bits-ui';

  import type { AlertDialogOverlayProps } from './types';

  let {
    ref = $bindable(null),
    class: className,
    ...rest
  }: AlertDialogOverlayProps = $props();
</script>

<AlertDialogPrimitive.Overlay
  bind:ref
  data-slot="alert-dialog-overlay"
  class={[
    'fixed inset-0 z-50 bg-black/50 data-[state=closed]:animate-fade-out data-[state=open]:animate-fade-in',
    className,
  ]}
  {...rest}
/>
