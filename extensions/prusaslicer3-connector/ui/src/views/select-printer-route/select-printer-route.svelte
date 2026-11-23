<!--
  The page the host mounts at /m/<namespace>/select-printer: the print
  dialog's store, reading the printers for as long as the page is open, drawn
  by SelectPrinterPage.
-->
<script lang="ts">
  import { slicer } from '@prusa/connect-ui/slicer';

  import { PrinterPicker } from '../../select-printer.svelte';
  import { heldSession } from '../../session';
  import { SelectPrinterPage } from '../select-printer-page';

  const { host, api } = heldSession();
  const picker = new PrinterPicker(api, slicer);

  $effect(() => {
    picker.start();

    return () => {
      picker.dispose();
    };
  });
</script>

<SelectPrinterPage view={picker} t={host.t} />
