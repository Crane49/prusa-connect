<!-- The head of the table. Its row carries the rule under the column names. -->
<script lang="ts">
  import type { TableHeaderProps } from './types';

  let { class: className, children, ...rest }: TableHeaderProps = $props();
</script>

<thead
  data-slot="table-header"
  class={['[&_tr]:border-b [&_tr]:border-border', className]}
  {...rest}
>
  {@render children?.()}
</thead>
