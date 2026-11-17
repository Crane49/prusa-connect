<!--
  A header cell.

  `scope` says which cells this one heads. It is `col` by default, which is
  what a cell in the header row is, and a header that names a row passes
  `scope="row"`. Without it a screen reader has to guess the association.

  `h-9` is the shared control height. A button, an input, a select and the tabs
  list are all that tall, so a control dropped into a header cell lines up with
  the column name beside it.
-->
<script lang="ts">
  import type { TableHeadProps } from './types';

  let {
    scope = 'col',
    class: className,
    children,
    ...rest
  }: TableHeadProps = $props();
</script>

<th
  data-slot="table-head"
  {scope}
  class={[
    'h-9 px-2 text-left align-middle text-xs font-semibold whitespace-nowrap text-muted-foreground [&:has([role=checkbox])]:pr-0 [&>[role=checkbox]]:translate-y-[2px]',
    className,
  ]}
  {...rest}
>
  {@render children?.()}
</th>
