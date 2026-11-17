<!--
  The one line that says what happened. It clamps to a single line, so a long
  title never pushes the description down the page; the description carries the
  detail.
-->
<script lang="ts">
  import type { AlertTitleProps } from './types';

  let { class: className, children, ...rest }: AlertTitleProps = $props();
</script>

<div
  data-slot="alert-title"
  class={['col-start-2 line-clamp-1 min-h-4 font-semibold', className]}
  {...rest}
>
  {@render children?.()}
</div>
