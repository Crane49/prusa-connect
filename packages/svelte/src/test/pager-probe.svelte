<!-- A pager over a listing that holds its own page and its own page size, for
     the pager tests. -->
<script lang="ts">
  import type { PageSize } from '../primitives/pager';
  import { DEFAULT_PAGE_SIZE, Pager } from '../primitives/pager';

  let {
    total = 61,
    unit,
    withCount = false,
  }: {
    total?: number;
    /** What the listing counts, in the form that suits `total`. */
    unit?: string;
    /** Whether the listing writes its own count in place of the sentence. */
    withCount?: boolean;
  } = $props();

  let pageSize = $state<PageSize>(DEFAULT_PAGE_SIZE);
  let page = $state(0);

  const pageCount = $derived(Math.max(1, Math.ceil(total / pageSize)));
</script>

{#snippet ownCount()}
  <p aria-live="polite" data-testid="own-count">Four of {total} match</p>
{/snippet}

<Pager
  {page}
  {pageCount}
  {pageSize}
  {total}
  {unit}
  count={withCount ? ownCount : undefined}
  onPageChange={(next: number) => (page = next)}
  onPageSizeChange={(next: PageSize) => {
    pageSize = next;
    page = 0;
  }}
/>
