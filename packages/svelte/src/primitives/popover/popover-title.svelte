<!--
  The heading of the surface. A div and not an `h2`, so the caller picks the
  level the surrounding page needs, and names the surface with `aria-labelledby`
  when it wants one.
-->
<script lang="ts">
  import type { PopoverTitleProps } from './types';

  let { children, class: className, ...rest }: PopoverTitleProps = $props();
</script>

<div data-slot="popover-title" class={['font-semibold', className]} {...rest}>
  {@render children?.()}
</div>
