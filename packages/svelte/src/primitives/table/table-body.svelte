<!--
  The rows. The last one drops its rule, so the body does not draw a line
  against the footer or the edge of the table.
-->
<script lang="ts">
  import type { TableBodyProps } from './types';

  let { class: className, children, ...rest }: TableBodyProps = $props();
</script>

<tbody
  data-slot="table-body"
  class={['[&_tr:last-child]:border-0', className]}
  {...rest}
>
  {@render children?.()}
</tbody>
