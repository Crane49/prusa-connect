<!--
  The prints that end next, soonest first: which printer, how far along, how
  long is left and the time of day it ends.

  It is the list a person reads to plan the next walk to the printers, so the
  time of day comes first and the bar under it says how sure that is.
-->
<script lang="ts">
  import { formatDate, formatDuration } from '../../../i18n';
  import { EmptyState, TilePanel } from '../../../primitives';
  import HourglassIcon from '@lucide/svelte/icons/hourglass';

  import { ordered, selected } from '../printers';
  import { t } from '../../../i18n';
  import { dashboardContext } from '../context';
  import { finishingSettings } from './config';
  import FleetState from './fleet-state.svelte';
  import JobBar from './job-bar.svelte';
  import type { CardProps } from '../../../slots';

  let { config, title }: CardProps = $props();

  const { clock, printers } = dashboardContext();

  const settings = $derived(finishingSettings(config));

  const views = $derived(
    ordered(
      printers.views.filter(
        (view) => view.remaining !== undefined && selected(view, settings),
      ),
      'remaining',
    ).slice(0, settings.limit),
  );

  $effect(() => clock.hold());
</script>

<TilePanel {title} fallback={t('dashboard.finishing.title')}>
  <FleetState>
    {#if views.length === 0}
      <EmptyState
        size="sm"
        icon={HourglassIcon}
        heading={t('dashboard.finishing.none')}
        description={t('dashboard.finishing.noneNote')}
      />
    {:else}
      <ol
        data-part="finishing"
        class="-me-1 flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto pe-1"
      >
        {#each views as view (view.id)}
          <li>
            <a
              href={view.detailHref}
              class="flex flex-col gap-1 rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              <span class="flex items-baseline justify-between gap-3">
                <span class="min-w-0 truncate text-sm font-medium"
                  >{view.name}</span
                >
                <span
                  class="shrink-0 font-mono text-sm font-semibold tabular-nums"
                >
                  {formatDate(clock.minute + (view.remaining ?? 0) * 1000, {
                    timeStyle: 'short',
                  })}
                </span>
              </span>
              <JobBar {view} />
              <span
                class="flex items-baseline justify-between gap-3 font-mono text-xs text-muted-foreground"
              >
                <span class="min-w-0 truncate">{view.jobName ?? ''}</span>
                <span class="shrink-0 tabular-nums">
                  {t('dashboard.printer.left', {
                    time: formatDuration(view.remaining ?? 0),
                  })}
                </span>
              </span>
            </a>
          </li>
        {/each}
      </ol>
    {/if}
  </FleetState>
</TilePanel>
