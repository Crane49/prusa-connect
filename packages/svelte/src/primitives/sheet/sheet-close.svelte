<!--
  A control that shuts the panel. The panel draws its own cross in the corner,
  so this is for a caller's own button, such as Cancel in the footer.

  It carries no class. bits-ui renders a bare `button`, and a caller that wants
  the product's Button draws it through the `child` snippet.
-->
<script lang="ts">
  import { Dialog as SheetPrimitive } from 'bits-ui';

  import type { SheetCloseProps } from './types';

  let { ref = $bindable(null), ...rest }: SheetCloseProps = $props();
</script>

<SheetPrimitive.Close bind:ref data-slot="sheet-close" {...rest} />
