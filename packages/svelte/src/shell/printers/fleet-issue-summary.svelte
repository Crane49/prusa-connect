<!--
  What is wrong across the whole fleet, and the filter to the printers it is
  wrong on. Each chip shows only its icon and count to fit the toolbar row; the
  full phrase is its name and its tooltip.
-->
<script lang="ts">
  import CircleAlertIcon from '@lucide/svelte/icons/circle-alert';
  import InfoIcon from '@lucide/svelte/icons/info';
  import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';

  import { formatNumber, t } from '../../i18n';
  import {
    Toggle,
    Tooltip,
    TooltipContent,
    TooltipProvider,
    TooltipTrigger,
  } from '../../primitives';
  import type { FleetIssueSummaryProps } from './types';

  let { counts, selected, onSelectedChange }: FleetIssueSummaryProps = $props();

  const SEVERITIES = [
    {
      id: 'error',
      label: 'printers.issues.error',
      icon: CircleAlertIcon,
      tone: 'text-destructive',
    },
    {
      id: 'warning',
      label: 'printers.issues.warning',
      icon: TriangleAlertIcon,
      tone: 'text-warning',
    },
    {
      id: 'info',
      label: 'printers.issues.info',
      icon: InfoIcon,
      tone: 'text-muted-foreground',
    },
  ] as const;

  const shown = $derived(
    SEVERITIES.filter((one) => (counts?.[one.id] ?? 0) > 0),
  );

  const toggle = (id: string): void => {
    onSelectedChange(
      selected.includes(id)
        ? selected.filter((held) => held !== id)
        : [...selected, id],
    );
  };
</script>

{#if shown.length > 0}
  <TooltipProvider delayDuration={300}>
    <div
      class="flex items-center gap-1"
      role="group"
      aria-label={t('printers.issues.label')}
    >
      {#each shown as severity (severity.id)}
        {@const Icon = severity.icon}
        {@const count = counts?.[severity.id] ?? 0}
        {@const phrase = t(severity.label, { count })}
        <Tooltip>
          <TooltipTrigger>
            {#snippet child({ props })}
              <Toggle
                {...props}
                size="sm"
                aria-label={phrase}
                pressed={selected.includes(severity.id)}
                onPressedChange={() => {
                  toggle(severity.id);
                }}
                class="tabular-nums"
              >
                <Icon aria-hidden="true" class={severity.tone} />
                {formatNumber(count)}
              </Toggle>
            {/snippet}
          </TooltipTrigger>
          <TooltipContent>{phrase}</TooltipContent>
        </Tooltip>
      {/each}
    </div>
  </TooltipProvider>
{/if}
