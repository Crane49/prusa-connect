<!--
  One printer's metrics: the names that arrived in the window with their
  newest value, and the chosen one drawn over time.
-->
<script lang="ts">
  import {
    EmptyState,
    FaultAlert,
    Label,
    LineChart,
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
    Skeleton,
  } from '@prusa/connect-ui/primitives';
  import ChartLineIcon from '@lucide/svelte/icons/chart-line';
  import { onMount, untrack } from 'svelte';

  import { faultText } from '../faults';
  import { MetricsStore } from '../metrics.svelte';
  import { originOf, originValue, WINDOWS, type Window } from '../presentation';
  import type { MetricsChoice } from '../page-state';
  import { heldSession } from '../session';

  const { host, api, status } = heldSession();
  const t = host.t;
  const uid = $props.id();

  let {
    start,
    onchoice,
  }: {
    /** The choices the page opens with, read once. */
    start?: MetricsChoice | undefined;
    /** Told after each change of the choices, for the page to keep them. */
    onchoice?: ((choice: MetricsChoice) => void) | undefined;
  } = $props();

  const metrics = new MetricsStore(api, Date.now, untrack(() => start) ?? {});

  // Told once the store has settled, because the store picks the first
  // metric itself when the one asked for did not arrive.
  const told = async (change: Promise<void>): Promise<void> => {
    await change;
    onchoice?.({
      origin: metrics.origin,
      window: metrics.window,
      name: metrics.chosen,
      field: metrics.field,
    });
  };

  onMount(() => {
    void metrics.load();

    return () => {
      metrics.dispose();
    };
  });

  /** Reads the names and the chosen series again, for the page's refresh. */
  export const reload = (): Promise<void> => metrics.load();

  const origins = $derived([
    { value: 'all', label: t('buddyDebugLogs.filter.allPrinters') },
    ...host.fleet.devices.map((one) => ({
      value: originValue({ kind: 'device', id: one.id }),
      label: one.name || one.id,
    })),
    ...status.unmatched.map((one) => ({
      value: originValue({ kind: 'mac', mac: one.mac }),
      label: t('buddyDebugLogs.filter.unmatched', { mac: one.mac }),
    })),
  ]);

  const windows = $derived(
    WINDOWS.map((hours) => ({
      value: String(hours),
      label: t('buddyDebugLogs.filter.windowHours', { hours }),
    })),
  );

  const lastOf = (last: Record<string, unknown>, field: string): string => {
    const value = last[field] ?? Object.values(last)[0];
    if (typeof value === 'string') return value;
    if (typeof value === 'number' || typeof value === 'boolean') {
      return String(value);
    }

    return value === undefined || value === null ? '' : JSON.stringify(value);
  };

  const names = $derived(
    (metrics.names ?? []).map((one) => ({
      value: one.name,
      label: t('buddyDebugLogs.metrics.last', {
        name: one.name,
        value: lastOf(one.last, one.fields[0] ?? 'v'),
      }),
    })),
  );

  const fields = $derived(
    metrics.fields.map((field) => ({ value: field, label: field })),
  );

  const series = $derived(
    metrics.chosen === undefined || metrics.points === undefined
      ? []
      : [
          {
            id: metrics.chosen,
            label: `${metrics.chosen} ${metrics.field}`,
            points: metrics.points.map((point) => ({
              at: Date.parse(point.at),
              value: point.value,
            })),
          },
        ],
  );

  const fault = $derived(
    metrics.fault === undefined ? undefined : faultText(metrics.fault, t),
  );
</script>

<div class="grid gap-3" data-part="metrics-view">
  <div class="flex flex-wrap items-end gap-2">
    <div class="grid gap-1">
      <Label for={`${uid}-origin`} variant="hint"
        >{t('buddyDebugLogs.filter.origin')}</Label
      >
      <Select
        value={originValue(metrics.origin)}
        items={origins}
        onValueChange={(next: string) => {
          void told(metrics.load({ origin: originOf(next) }));
        }}
      >
        <SelectTrigger id={`${uid}-origin`} size="sm" class="min-w-44">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {#each origins as origin (origin.value)}
            <SelectItem value={origin.value} label={origin.label}
              >{origin.label}</SelectItem
            >
          {/each}
        </SelectContent>
      </Select>
    </div>
    <div class="grid gap-1">
      <Label for={`${uid}-window`} variant="hint"
        >{t('buddyDebugLogs.filter.window')}</Label
      >
      <Select
        value={String(metrics.window)}
        items={windows}
        onValueChange={(next: string) => {
          void told(metrics.load({ window: Number(next) as Window }));
        }}
      >
        <SelectTrigger id={`${uid}-window`} size="sm" class="min-w-36">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {#each windows as window (window.value)}
            <SelectItem value={window.value} label={window.label}
              >{window.label}</SelectItem
            >
          {/each}
        </SelectContent>
      </Select>
    </div>
    {#if names.length > 0}
      <div class="grid gap-1">
        <Label for={`${uid}-metric`} variant="hint"
          >{t('buddyDebugLogs.metrics.metric')}</Label
        >
        <Select
          value={metrics.chosen ?? ''}
          items={names}
          onValueChange={(next: string) => {
            void told(metrics.choose(next));
          }}
        >
          <SelectTrigger id={`${uid}-metric`} size="sm" class="min-w-56">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {#each names as name (name.value)}
              <SelectItem value={name.value} label={name.label}
                >{name.label}</SelectItem
              >
            {/each}
          </SelectContent>
        </Select>
      </div>
    {/if}
    {#if fields.length > 1}
      <div class="grid gap-1">
        <Label for={`${uid}-field`} variant="hint"
          >{t('buddyDebugLogs.metrics.field')}</Label
        >
        <Select
          value={metrics.field}
          items={fields}
          onValueChange={(next: string) => {
            void told(metrics.choose(metrics.chosen ?? '', next));
          }}
        >
          <SelectTrigger id={`${uid}-field`} size="sm" class="min-w-28">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {#each fields as field (field.value)}
              <SelectItem value={field.value} label={field.label}
                >{field.label}</SelectItem
              >
            {/each}
          </SelectContent>
        </Select>
      </div>
    {/if}
  </div>

  <FaultAlert {fault} />

  {#if metrics.truncated}
    <p class="text-sm text-muted-foreground" data-part="metrics-truncated">
      {t('buddyDebugLogs.metrics.truncated')}
    </p>
  {/if}

  {#if metrics.names === undefined}
    {#if fault === undefined}
      <Skeleton class="h-64" />
    {/if}
  {:else if metrics.names.length === 0}
    <EmptyState
      icon={ChartLineIcon}
      headingLevel="h3"
      heading={t('buddyDebugLogs.metrics.empty.heading')}
      description={t('buddyDebugLogs.metrics.empty.note')}
      data-part="metrics-empty"
    />
  {:else if series.length > 0 && (series[0]?.points.length ?? 0) > 0}
    <LineChart
      class="h-64"
      {series}
      label={t('buddyDebugLogs.metrics.chart', { name: metrics.chosen ?? '' })}
    />
  {:else if metrics.points !== undefined}
    <p class="text-sm text-muted-foreground" data-part="metrics-no-points">
      {t('buddyDebugLogs.metrics.noPoints')}
    </p>
  {/if}
</div>
