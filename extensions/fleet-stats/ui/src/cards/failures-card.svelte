<!--
  A dashboard card of the newest failed and cancelled jobs, with the printer
  each one ran on.
-->
<script lang="ts">
  import type { CardProps } from '@prusa/connect-ui/slots';

  import type { JobStats } from '../domain/job-stats';
  import { heldHost } from '../host';
  import FailureList from '../view/failure-list.svelte';
  import CardFrame from './card-frame.svelte';
  import { cardViewOf, failureLimitOf } from './settings';

  let { config, title, editing }: CardProps = $props();

  const host = heldHost();
  const t = host.t;
  const view = $derived(cardViewOf(config));
  const limit = $derived(failureLimitOf(config));
  const printerName = (id: string) => host.fleet.device(id)?.name ?? id;
</script>

<CardFrame
  heading={title.trim() === '' ? t('fleet-stats.section.recent') : title}
  {view}
  {editing}
>
  {#snippet children(stats: JobStats)}
    {#if stats.recentFailures.length === 0}
      <p class="text-sm text-muted-foreground">
        {t('fleet-stats.cards.failures.none')}
      </p>
    {:else}
      <div class="min-h-0 flex-1 overflow-auto">
        <FailureList
          jobs={stats.recentFailures.slice(0, limit)}
          {printerName}
        />
      </div>
    {/if}
  {/snippet}
</CardFrame>
