<script lang="ts">
  import type { SheetHeaderProps } from './types';

  let { children, class: className, ...rest }: SheetHeaderProps = $props();
</script>

<div
  data-slot="sheet-header"
  class={['flex flex-col gap-1.5 p-4', className]}
  {...rest}
>
  {@render children?.()}
</div>
