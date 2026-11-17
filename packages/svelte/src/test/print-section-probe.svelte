<!-- A print-section that reads the dialog it is drawn in, for the slot tests. -->
<script lang="ts">
  import { printDialogContext } from '../slots/print-dialog-context';

  let { fileRef }: { fileRef: string } = $props();

  const dialog = printDialogContext();
</script>

<p data-testid="print-section">
  {fileRef} for {dialog.selected.join(',') || 'nothing'}
</p>
<button type="button" onclick={() => dialog.close()}>Close from section</button>
