<!--
  Rows of data with a header.

  The table sits in a box that scrolls sideways, so a table wider than its
  column keeps its columns at their own width instead of squeezing every cell.
  The caller's class goes on the table, so a width or a layout the caller sets
  applies to the table and not to the box around it.
-->
<script lang="ts">
  import type { TableProps } from './types';

  let { class: className, children, ...rest }: TableProps = $props();
</script>

<div data-slot="table-container" class="relative w-full overflow-x-auto">
  <table
    data-slot="table"
    class={['w-full caption-bottom text-sm', className]}
    {...rest}
  >
    {@render children?.()}
  </table>
</div>
