<script lang="ts">
  import { Card, CardContent } from '@prusa/connect-ui/primitives';
  import { formatNumber } from '@prusa/connect-ui/i18n';

  import { statsFor } from './data/stats.svelte';
  import { heldHost } from './host';
  import { UtilizationGrid } from './utilization-grid';
  import FailureList from './view/failure-list.svelte';
  import { searchOf } from './view/page-query';

  interface Props {
    deviceId: string;
  }

  let { deviceId }: Props = $props();
  const host = heldHost();
  const t = host.t;
  const stats = statsFor(host);

  const summary = $derived(
    stats.state.kind === 'ready' ? stats.state.stats : undefined,
  );
  const row = $derived(summary?.byDevice.find((r) => r.key === deviceId));
  const grid = $derived(
    summary?.utilization.filter((r) => r.deviceId === deviceId) ?? [],
  );
  const recent = $derived(
    summary?.recentFailures
      .filter((job) => job.deviceId === deviceId)
      .slice(0, 3) ?? [],
  );
  const pageHref = $derived(
    `${host.pageHref('fleet-stats')}${searchOf({ days: stats.days, printer: deviceId, tag: '' })}`,
  );
</script>

{#if row !== undefined}
  <Card>
    <CardContent class="flex flex-col gap-6">
      <div class="flex items-baseline justify-between gap-2">
        <p class="text-xs text-muted-foreground">
          {t('fleet-stats.section.period', { days: stats.days })}
        </p>
        <a class="text-sm hover:underline" href={pageHref}
          >{t('fleet-stats.section.open')}</a
        >
      </div>
      <UtilizationGrid rows={grid} days={false} />
      <p class="text-sm">
        {t('fleet-stats.section.summary', {
          success:
            row.successRate === undefined
              ? t('fleet-stats.none')
              : formatNumber(row.successRate, {
                  style: 'percent',
                  maximumFractionDigits: 0,
                }),
          jobs: row.jobs,
        })}
      </p>
      {#if recent.length > 0}
        <div class="flex flex-col gap-1">
          <h4 class="text-xs text-muted-foreground">
            {t('fleet-stats.section.recent')}
          </h4>
          <FailureList jobs={recent} />
        </div>
      {/if}
    </CardContent>
  </Card>
{/if}
