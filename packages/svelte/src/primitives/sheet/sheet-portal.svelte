<!--
  Where the panel and its scrim are drawn: at the end of the body, so no
  ancestor's `overflow` or stacking context can clip it. It renders no element
  of its own.
-->
<script lang="ts">
  import { Dialog as SheetPrimitive } from 'bits-ui';

  import type { SheetPortalProps } from './types';

  let { ...rest }: SheetPortalProps = $props();
</script>

<SheetPrimitive.Portal {...rest} />
