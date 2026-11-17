<!--
  The panel itself, with its scrim, in a portal so no ancestor's `overflow` or
  stacking context can clip it.

  `size` is a data attribute and not a class recipe. The header, the footer and
  the title all read it through `group-data-[size=...]`, so one attribute on the
  panel lays out everything inside it.

  `group/alert-dialog-content` is what those selectors name.
-->
<script lang="ts">
  import { AlertDialog as AlertDialogPrimitive } from 'bits-ui';

  import AlertDialogOverlay from './alert-dialog-overlay.svelte';
  import AlertDialogPortal from './alert-dialog-portal.svelte';
  import type { AlertDialogContentProps } from './types';

  let {
    ref = $bindable(null),
    class: className,
    size = 'default',
    ...rest
  }: AlertDialogContentProps = $props();
</script>

<AlertDialogPortal>
  <AlertDialogOverlay />
  <AlertDialogPrimitive.Content
    bind:ref
    data-slot="alert-dialog-content"
    data-size={size}
    class={[
      'group/alert-dialog-content fixed top-[50%] left-[50%] z-50 grid w-full max-w-[calc(100%-2rem)] translate-x-[-50%] translate-y-[-50%] gap-4 rounded-sm border border-border bg-background p-6 data-[size=sm]:max-w-xs data-[state=closed]:animate-scale-out data-[state=open]:animate-scale-in data-[size=default]:sm:max-w-lg',
      className,
    ]}
    {...rest}
  />
</AlertDialogPortal>
