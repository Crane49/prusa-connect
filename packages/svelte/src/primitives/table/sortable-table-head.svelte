<!--
  A column header that sorts its column.

  The sort order itself stays with the caller: it passes the current
  `direction` and takes the click in `onSort`. `aria-sort` is `none` while the
  column is not the one sorted, so a reader hears that it can be.
-->
<script lang="ts">
  import ArrowDownIcon from '@lucide/svelte/icons/arrow-down';
  import ArrowUpIcon from '@lucide/svelte/icons/arrow-up';
  import ArrowUpDownIcon from '@lucide/svelte/icons/arrow-up-down';

  import TableHead from './table-head.svelte';
  import type { SortableTableHeadProps } from './types';

  let {
    label,
    direction,
    onSort,
    align = 'start',
    class: className,
  }: SortableTableHeadProps = $props();

  const end = $derived(align === 'end');
  const GLYPH = {
    ascending: ArrowUpIcon,
    descending: ArrowDownIcon,
    none: ArrowUpDownIcon,
  };
  const Glyph = $derived(GLYPH[direction ?? 'none']);
</script>

<TableHead
  aria-sort={direction ?? 'none'}
  class={[end && 'text-end', className]}
>
  <button
    type="button"
    class={[
      'inline-flex items-center gap-1 rounded-sm outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background',
      end && 'flex-row-reverse',
    ]}
    onclick={onSort}
  >
    {label}
    <Glyph
      aria-hidden="true"
      class={['size-3', direction === undefined && 'opacity-50']}
    />
  </button>
</TableHead>
