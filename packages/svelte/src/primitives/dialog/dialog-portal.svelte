<!--
  Where the panel is drawn: at the end of the body, so no ancestor's `overflow`
  or stacking context can clip it. It renders no element of its own.
-->
<script lang="ts">
  import { Dialog as DialogPrimitive } from 'bits-ui';

  import type { DialogPortalProps } from './types';

  let { ...rest }: DialogPortalProps = $props();
</script>

<DialogPrimitive.Portal {...rest} />
