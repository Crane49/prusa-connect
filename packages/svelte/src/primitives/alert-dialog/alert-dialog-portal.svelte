<!--
  Where the panel is drawn: at the end of the body, so no ancestor's `overflow`
  or stacking context can clip it. It renders no element of its own.
-->
<script lang="ts">
  import { AlertDialog as AlertDialogPrimitive } from 'bits-ui';

  import type { AlertDialogPortalProps } from './types';

  let { ...rest }: AlertDialogPortalProps = $props();
</script>

<AlertDialogPrimitive.Portal {...rest} />
