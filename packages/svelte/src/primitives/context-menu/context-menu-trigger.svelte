<!--
  The region the right button opens the menu over.

  bits-ui gives it `tabindex="-1"`, so it is not a tab stop of its own. A caller
  that wants one passes `tabindex={0}`, which bits-ui merges last and therefore
  wins.
-->
<script lang="ts">
  import { ContextMenu as ContextMenuPrimitive } from 'bits-ui';

  import type { ContextMenuTriggerProps } from './types';

  let { ref = $bindable(null), ...rest }: ContextMenuTriggerProps = $props();
</script>

<ContextMenuPrimitive.Trigger
  bind:ref
  data-slot="context-menu-trigger"
  {...rest}
/>
