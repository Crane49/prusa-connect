<script lang="ts">
  import type { PopoverHeaderProps } from './types';

  let { children, class: className, ...rest }: PopoverHeaderProps = $props();
</script>

<div
  data-slot="popover-header"
  class={['flex flex-col gap-1 text-sm', className]}
  {...rest}
>
  {@render children?.()}
</div>
