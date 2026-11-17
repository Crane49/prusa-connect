<!--
  A dashboard card of the hour-by-weekday heatmap: when the printers print.
-->
<script lang="ts">
  import type { CardProps } from '@prusa/connect-ui/slots';

  import type { JobStats } from '../domain/job-stats';
  import { heldHost } from '../host';
  import { UtilizationGrid } from '../utilization-grid';
  import CardFrame from './card-frame.svelte';
  import { cardViewOf } from './settings';

  let { config, title, editing }: CardProps = $props();

  const t = heldHost().t;
  const view = $derived(cardViewOf(config));
</script>

<CardFrame
  heading={title.trim() === '' ? t('fleet-stats.utilization.heading') : title}
  {view}
  {editing}
>
  {#snippet children(stats: JobStats)}
    <div class="min-h-0 flex-1 overflow-auto">
      <UtilizationGrid rows={stats.utilization} compact />
    </div>
  {/snippet}
</CardFrame>
