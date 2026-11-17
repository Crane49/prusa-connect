<!--
  The row of controls at the foot of the panel. `mt-auto` pushes it to the
  bottom edge whatever the panel holds above it.
-->
<script lang="ts">
  import type { SheetFooterProps } from './types';

  let { children, class: className, ...rest }: SheetFooterProps = $props();
</script>

<div
  data-slot="sheet-footer"
  class={['mt-auto flex flex-col gap-2 p-4', className]}
  {...rest}
>
  {@render children?.()}
</div>
