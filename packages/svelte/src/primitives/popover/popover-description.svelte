<script lang="ts">
  import type { PopoverDescriptionProps } from './types';

  let {
    children,
    class: className,
    ...rest
  }: PopoverDescriptionProps = $props();
</script>

<p
  data-slot="popover-description"
  class={['text-muted-foreground', className]}
  {...rest}
>
  {@render children?.()}
</p>
