<script lang="ts">
  import { formatNumber } from '@prusa/connect-ui/i18n';

  import { statsFor } from './data/stats.svelte';
  import { heldHost } from './host';

  interface Props {
    deviceId: string;
  }

  let { deviceId }: Props = $props();
  const host = heldHost();
  const t = host.t;
  const stats = statsFor(host);

  const row = $derived(
    stats.state.kind === 'ready'
      ? stats.state.stats.byDevice.find((r) => r.key === deviceId)
      : undefined,
  );
</script>

{#if row !== undefined && row.jobs > 0}
  <span class="text-sm text-muted-foreground">
    {t('fleet-stats.card.line', {
      success:
        row.successRate === undefined
          ? t('fleet-stats.none')
          : formatNumber(row.successRate, {
              style: 'percent',
              maximumFractionDigits: 0,
            }),
      jobs: row.jobs,
      days: stats.days,
    })}
  </span>
{/if}
