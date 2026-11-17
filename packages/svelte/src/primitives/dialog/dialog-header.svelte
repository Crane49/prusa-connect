<script lang="ts">
  import type { DialogHeaderProps } from './types';

  let { children, class: className, ...rest }: DialogHeaderProps = $props();
</script>

<div
  data-slot="dialog-header"
  class={['flex flex-col gap-2 text-center sm:text-left', className]}
  {...rest}
>
  {@render children?.()}
</div>
