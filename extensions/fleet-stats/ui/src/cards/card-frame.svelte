<!--
  The frame every card of this module draws in: the heading and what it
  counts, the link to the page, and the statistics under it once they are
  read, given what is left of the card's height.
-->
<script lang="ts">
  import { describeFault } from '@prusa/connect-ui/i18n';
  import {
    FaultAlert,
    Skeleton,
    TilePanel,
  } from '@prusa/connect-ui/primitives';
  import { moduleIcon } from '@prusa/connect-ui/slots';
  import type { Snippet } from 'svelte';

  import { statsFor } from '../data/stats.svelte';
  import type { JobStats } from '../domain/job-stats';
  import { heldHost } from '../host';
  import { type CardView, pageSearchOf } from './settings';
  import WantPeriod from './want-period.svelte';

  let {
    heading,
    view,
    editing,
    children,
  }: {
    heading: string;
    view: CardView;
    editing: boolean;
    children: Snippet<[JobStats]>;
  } = $props();

  const host = heldHost();
  const t = host.t;
  const stats = statsFor(host);
  // The page's own icon, which the library draws without this module
  // carrying an icon set of its own.
  const PageIcon = moduleIcon('chart-line');

  const current = $derived(stats.view(view.days, view.scope));
  const who = $derived.by(() => {
    const { printers, tag } = view.scope;
    if (printers.length === 1) {
      const id = printers[0] ?? '';
      return host.fleet.device(id)?.name ?? id;
    }
    if (printers.length > 1)
      return t('fleet-stats.cards.printers', { count: printers.length });
    if (tag !== '') return t('fleet-stats.cards.tag', { tag });
    return t('fleet-stats.cards.fleet');
  });
</script>

{#key view.days}
  <WantPeriod {stats} days={view.days} />
{/key}

<TilePanel
  title={heading}
  subline={t('fleet-stats.cards.counts', { who, days: view.days })}
>
  {#snippet trailing()}
    {#if !editing}
      <a
        href={`${host.pageHref('fleet-stats')}${pageSearchOf(view)}`}
        class="shrink-0 rounded-sm p-1 text-muted-foreground hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
        aria-label={t('fleet-stats.cards.open')}
        data-part="dashboard-open"
      >
        <PageIcon aria-hidden="true" class="size-4" />
      </a>
    {/if}
  {/snippet}
  {#if current.kind === 'ready'}
    {@render children(current.stats)}
  {:else if current.kind === 'loading'}
    <Skeleton
      class="h-16"
      role="status"
      aria-label={t('fleet-stats.cards.loading')}
    />
  {:else}
    <FaultAlert fault={describeFault(current.thrown)} />
  {/if}
</TilePanel>
