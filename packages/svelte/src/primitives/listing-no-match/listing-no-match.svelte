<!--
  What a listing says when its search and filters match no row: the way back
  to every row is the one action.
-->
<script lang="ts">
  import SearchXIcon from '@lucide/svelte/icons/search-x';

  import { t } from '../../i18n';
  import { Button } from '../button';
  import { EmptyState } from '../empty-state';
  import type { ListingNoMatchProps } from './types';

  let {
    heading,
    description,
    headingLevel,
    onClear,
    clearLabel,
    class: className,
  }: ListingNoMatchProps = $props();
</script>

{#snippet action()}
  <Button variant="secondary" size="sm" onclick={() => onClear()}>
    {clearLabel ?? t('primitives.listing.clearAll')}
  </Button>
{/snippet}

<EmptyState
  icon={SearchXIcon}
  {heading}
  {headingLevel}
  {description}
  {action}
  class={className}
/>
