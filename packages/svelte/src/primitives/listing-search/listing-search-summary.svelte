<!-- What a search matched, and the one press back to the whole listing. -->
<script lang="ts">
  import { Button } from '../button';
  import type { ListingSearchSummaryProps } from './types';

  let {
    summary,
    clearLabel,
    onClear,
    class: className,
  }: ListingSearchSummaryProps = $props();
</script>

<div class={['flex min-w-0 items-center gap-2', className]}>
  <p class="text-sm text-muted-foreground">{summary}</p>
  <Button variant="ghost" size="sm" onclick={onClear}>{clearLabel}</Button>
</div>
