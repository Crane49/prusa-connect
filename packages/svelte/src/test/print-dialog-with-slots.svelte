<!-- PrintDialog under a slot context, as the app's composition root puts it. -->
<script lang="ts">
  import PrintDialog from '../shell/print-picker/print-dialog.svelte';
  import type { PrintDialogProps } from '../shell/print-picker/types';
  import { setSlotContext } from '../slots/context';
  import type { SlotRegistry } from '../slots/registry.svelte';

  let { registry, ...props }: PrintDialogProps & { registry: SlotRegistry } =
    $props();

  setSlotContext({
    get registry() {
      return registry;
    },
  });
</script>

<PrintDialog {...props} />
