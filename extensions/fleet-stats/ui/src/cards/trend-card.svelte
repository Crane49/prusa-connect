<!--
  A dashboard card of one metric day by day: jobs, success or print hours.
-->
<script lang="ts">
  import type { CardProps } from '@prusa/connect-ui/slots';

  import type { JobStats } from '../domain/job-stats';
  import { heldHost } from '../host';
  import TrendChart from '../view/trend-chart.svelte';
  import { trendMetricOf } from '../view/trend';
  import CardFrame from './card-frame.svelte';
  import { cardViewOf } from './settings';

  let { config, title, editing }: CardProps = $props();

  const t = heldHost().t;
  const view = $derived(cardViewOf(config));
  const metric = $derived(trendMetricOf(config.metric));
</script>

<CardFrame
  heading={title.trim() === ''
    ? t('fleet-stats.cards.trend.heading', {
        metric: t(`fleet-stats.trend.metric.${metric}`),
      })
    : title}
  {view}
  {editing}
>
  {#snippet children(stats: JobStats)}
    <TrendChart trend={stats.trend} {metric} class="min-h-0 flex-1" />
  {/snippet}
</CardFrame>
