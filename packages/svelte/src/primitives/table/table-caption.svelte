<!--
  What the table lists, read out before the rows. It draws under the table and
  a screen reader announces it with the table.
-->
<script lang="ts">
  import type { TableCaptionProps } from './types';

  let { class: className, children, ...rest }: TableCaptionProps = $props();
</script>

<caption
  data-slot="table-caption"
  class={['mt-4 text-sm text-muted-foreground', className]}
  {...rest}
>
  {@render children?.()}
</caption>
