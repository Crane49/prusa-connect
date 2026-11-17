<!--
  Whether each port is receiving, what arrived, and the room it takes. A port
  the operator did not allow is the one thing that stops every printer at
  once, so it is said in words above the cards and never left to a badge.

  The cards fold under one row that still names each stream's state and what
  it kept, so a folded strip answers "is it receiving" at a glance.
-->
<script lang="ts">
  import {
    formatBytes,
    formatDate,
    formatNumber,
  } from '@prusa/connect-ui/i18n';
  import {
    Alert,
    AlertDescription,
    Badge,
    type BadgeVariant,
    Collapsible,
    CollapsibleContent,
    CollapsibleTrigger,
    FaultAlert,
    KeyValueList,
    Skeleton,
  } from '@prusa/connect-ui/primitives';

  import type { StreamStatus } from '../api';
  import { heldSession } from '../session';
  import { streamState } from '../status.svelte';

  let {
    open = false,
    onOpenChange,
  }: {
    open?: boolean;
    /** Told when the person folds or unfolds the cards. */
    onOpenChange?: ((open: boolean) => void) | undefined;
  } = $props();

  const { status, host } = heldSession();
  const t = host.t;

  // Judged at the time the status was read, so a stream whose printer
  // stopped sending turns from receiving to waiting on the next read.
  const stateOf = (
    stream: StreamStatus,
  ): { variant: BadgeVariant; label: string } => {
    switch (streamState(stream, status.readAt)) {
      case 'notGranted':
        return {
          variant: 'warning',
          label: t('buddyDebugLogs.status.notGranted'),
        };
      case 'failed':
        return {
          variant: 'destructive',
          label: t('buddyDebugLogs.status.failed'),
        };
      case 'receiving':
        return {
          variant: 'success',
          label: t('buddyDebugLogs.status.listening'),
        };
      case 'waiting':
        return {
          variant: 'secondary',
          label: t('buddyDebugLogs.status.waiting'),
        };
    }
  };

  const lastOf = (stream: StreamStatus): string =>
    stream.last_received === undefined
      ? t('buddyDebugLogs.status.lastNever')
      : formatDate(stream.last_received, {
          month: 'short',
          day: 'numeric',
          hour: '2-digit',
          minute: '2-digit',
          second: '2-digit',
        });

  const ungranted = $derived(
    (status.status?.streams ?? []).some((one) => !one.listener.granted),
  );
</script>

<section class="grid gap-3" data-part="status">
  {#if status.status === undefined}
    <div class="grid gap-3 @2xl/logs:grid-cols-2">
      <Skeleton class="h-32" />
      <Skeleton class="h-32" />
    </div>
  {:else}
    {#if ungranted}
      <Alert variant="warning" data-part="not-granted">
        <AlertDescription
          >{t('buddyDebugLogs.status.notGrantedNote')}</AlertDescription
        >
      </Alert>
    {/if}
    {#if status.status.problem}
      <FaultAlert
        fault={{
          code: undefined,
          detail: undefined,
          title: t('buddyDebugLogs.status.storeProblem', {
            problem: status.status.problem,
          }),
        }}
      />
    {/if}
    <Collapsible
      {open}
      onOpenChange={(next: boolean) => {
        onOpenChange?.(next);
      }}
      data-part="status-fold"
    >
      <CollapsibleTrigger variant="row" class="flex-wrap gap-x-3">
        <span class="text-sm font-medium text-foreground"
          >{t('buddyDebugLogs.status.title')}</span
        >
        {#each status.status.streams as stream (stream.stream)}
          {@const state = stateOf(stream)}
          <span
            class="flex items-center gap-1.5 text-xs font-normal"
            data-part={`summary-${stream.stream}`}
          >
            {t(`buddyDebugLogs.stream.${stream.stream}`)}
            <Badge variant={state.variant}>{state.label}</Badge>
            <span class="text-muted-foreground tabular-nums">
              {t('buddyDebugLogs.status.keptShort', {
                count: formatNumber(stream.counts.records),
              })}
            </span>
          </span>
        {/each}
      </CollapsibleTrigger>
      <CollapsibleContent class="grid gap-3 pt-3">
        <div class="grid gap-3 @2xl/logs:grid-cols-2">
          {#each status.status.streams as stream (stream.stream)}
            {@const state = stateOf(stream)}
            <div
              class="rounded-lg grid gap-2 border border-border bg-card p-4"
              data-part={`stream-${stream.stream}`}
            >
              <div class="flex flex-wrap items-center justify-between gap-2">
                <h2 class="text-sm font-semibold">
                  {t(`buddyDebugLogs.stream.${stream.stream}`)}
                </h2>
                <Badge variant={state.variant}>{state.label}</Badge>
              </div>
              {#if stream.listener.error}
                <FaultAlert
                  fault={{
                    code: undefined,
                    detail: undefined,
                    title: stream.listener.error,
                  }}
                />
              {/if}
              <KeyValueList
                rows={[
                  {
                    id: 'port',
                    label: t('buddyDebugLogs.status.port', {
                      network: (stream.listener.network ?? 'udp').toUpperCase(),
                      port: String(stream.listener.port ?? ''),
                    }),
                    value: state.label,
                  },
                  {
                    id: 'last',
                    label: t('buddyDebugLogs.status.last'),
                    value: lastOf(stream),
                  },
                  {
                    id: 'received',
                    label: t('buddyDebugLogs.status.received'),
                    value: formatNumber(stream.counts.received),
                  },
                  {
                    id: 'kept',
                    label: t('buddyDebugLogs.status.kept'),
                    value: formatNumber(stream.counts.records),
                  },
                  {
                    id: 'lost',
                    label: t('buddyDebugLogs.status.unreadable'),
                    value: formatNumber(stream.counts.unreadable),
                  },
                  {
                    id: 'room',
                    label: t('buddyDebugLogs.status.roomLabel'),
                    value: t('buddyDebugLogs.status.room', {
                      used: formatBytes(stream.bytes),
                      limit: formatBytes(stream.limit),
                    }),
                  },
                ]}
              />
            </div>
          {/each}
        </div>
        <p class="text-xs text-muted-foreground">
          {t('buddyDebugLogs.status.retention', {
            hours: status.status.retention_hours,
            size: formatBytes(status.status.max_mib * 1024 * 1024),
          })}
        </p>
      </CollapsibleContent>
    </Collapsible>
  {/if}
</section>
