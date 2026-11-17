<!-- PrintDialog behind the button that opens it, as a page puts it, so a test
     sees where focus lands when the dialog opens rather than when it mounts. -->
<script lang="ts">
  import PrintDialog from '../shell/print-picker/print-dialog.svelte';
  import type { PrintDialogProps } from '../shell/print-picker/types';

  let props: Omit<PrintDialogProps, 'open' | 'onOpenChange'> = $props();

  let open = $state(false);
</script>

<button
  type="button"
  onclick={() => {
    open = true;
  }}
>
  Print this file
</button>
<PrintDialog
  {...props}
  {open}
  onOpenChange={(next: boolean) => {
    open = next;
  }}
/>
