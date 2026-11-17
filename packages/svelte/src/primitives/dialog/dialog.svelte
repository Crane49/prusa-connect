<!--
  A panel in the middle of the page that takes the whole interaction until it
  is answered. bits-ui carries the open state, the focus trap, the return of
  focus to the control that opened it, the escape key and the `data-state` the
  entry and exit animations read.

  The root renders no element, so it takes no `data-slot` and no class. Every
  class is on the parts below it.
-->
<script lang="ts">
  import { Dialog as DialogPrimitive } from 'bits-ui';

  import type { DialogProps } from './types';

  let { open = $bindable(false), ...rest }: DialogProps = $props();
</script>

<DialogPrimitive.Root bind:open {...rest} />
