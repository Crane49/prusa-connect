<!--
  The frame around a list of rows. It adds no padding and no separators; the
  rows draw their own.
-->
<script lang="ts">
  import type { RowListProps } from './types';

  let { children, class: className, ...rest }: RowListProps = $props();
</script>

<div
  data-slot="row-list"
  class={['rounded-sm border border-border', className]}
  {...rest}
>
  {@render children()}
</div>
