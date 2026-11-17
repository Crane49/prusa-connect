<!-- A summary row under the body, on a filled band. -->
<script lang="ts">
  import type { TableFooterProps } from './types';

  let { class: className, children, ...rest }: TableFooterProps = $props();
</script>

<tfoot
  data-slot="table-footer"
  class={[
    'border-t border-border bg-muted font-medium [&>tr]:last:border-b-0',
    className,
  ]}
  {...rest}
>
  {@render children?.()}
</tfoot>
