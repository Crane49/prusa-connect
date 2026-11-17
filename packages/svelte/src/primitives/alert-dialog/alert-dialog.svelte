<!--
  A question a person has to answer before anything else happens. bits-ui
  carries the open state, the focus trap, the escape key and the focus return,
  and it gives the panel `role="alertdialog"`.

  It differs from a dialog in two ways bits-ui sets for us: a press on the
  scrim does not shut it, and the panel itself takes the focus when it opens.

  The root renders no element, so it takes no `data-slot` and no class.
-->
<script lang="ts">
  import { AlertDialog as AlertDialogPrimitive } from 'bits-ui';

  import type { AlertDialogProps } from './types';

  let { open = $bindable(false), ...rest }: AlertDialogProps = $props();
</script>

<AlertDialogPrimitive.Root bind:open {...rest} />
