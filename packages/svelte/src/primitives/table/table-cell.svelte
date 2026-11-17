<!-- One cell of data. -->
<script lang="ts">
  import type { TableCellProps } from './types';

  let { class: className, children, ...rest }: TableCellProps = $props();
</script>

<td
  data-slot="table-cell"
  class={[
    'p-2 align-middle whitespace-nowrap [&:has([role=checkbox])]:pr-0 [&>[role=checkbox]]:translate-y-[2px]',
    className,
  ]}
  {...rest}
>
  {@render children?.()}
</td>
