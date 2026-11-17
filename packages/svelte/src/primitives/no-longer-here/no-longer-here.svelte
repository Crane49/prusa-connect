<!--
  What a detail page says when its object is gone, whether it was removed
  while the page was open or the page was opened on an address that no longer
  holds it. The page stays where it is and offers the way back.
-->
<script lang="ts">
  import SearchXIcon from '@lucide/svelte/icons/search-x';

  import { Button } from '../button';
  import { Card, CardContent } from '../card';
  import { EmptyState } from '../empty-state';
  import type { NoLongerHereProps } from './types';

  let {
    heading,
    description,
    headingLevel,
    linkLabel,
    href,
    onLink,
    class: className,
  }: NoLongerHereProps = $props();
</script>

{#snippet action()}
  {#if href !== undefined}
    <Button variant="secondary">
      {#snippet child({ props }: { props: Record<string, unknown> })}
        <a {...props} type={undefined} {href}>{linkLabel}</a>
      {/snippet}
    </Button>
  {:else}
    <Button variant="secondary" onclick={() => onLink?.()}>
      {linkLabel}
    </Button>
  {/if}
{/snippet}

<Card class={className}>
  <CardContent>
    <EmptyState
      icon={SearchXIcon}
      {heading}
      {headingLevel}
      {description}
      {action}
    />
  </CardContent>
</Card>
