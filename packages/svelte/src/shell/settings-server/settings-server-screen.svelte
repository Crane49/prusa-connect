<!--
  The Server room: whether every instance the configuration requires is
  serving, and how the printers behind each connector stand. A printer that is
  switched off is normal, so the second half never makes the first one fail,
  and the page says so rather than calling it a fault. A printer away from
  the server is named and links to its own page.
-->
<script lang="ts">
  import type { ConnectorRollup } from '../../data';
  import {
    describeFault,
    formatDateTime,
    formatList,
    formatNumber,
    t,
  } from '../../i18n';
  import {
    Badge,
    type BadgeVariant,
    Button,
    Card,
    CardContent,
    FaultAlert,
    SettingsPane,
    Skeleton,
  } from '../../primitives';
  import { printerHref } from '../printers';
  import type { SettingsServerScreenProps } from './types';

  let { source }: SettingsServerScreenProps = $props();

  // Every key is written out rather than assembled from a value.
  const VERDICT: Readonly<Record<string, string>> = {
    pass: 'settingsServer.verdict.pass',
    degraded: 'settingsServer.verdict.degraded',
    fail: 'settingsServer.verdict.fail',
  };
  const VERDICT_VARIANT: Readonly<Record<string, BadgeVariant>> = {
    pass: 'success',
    degraded: 'warning',
    fail: 'destructive',
  };
  const STATE: Readonly<Record<string, string>> = {
    connected: 'settingsServer.state.connected',
    connecting: 'settingsServer.state.connecting',
    disconnected: 'settingsServer.state.disconnected',
    error: 'settingsServer.state.error',
    unknown: 'settingsServer.state.unknown',
  };

  const stateText = (state: string): string => {
    const key = STATE[state];

    return key === undefined ? state : t(key);
  };

  // The server rolls "fail" up for anything under half connected; zero of some
  // is worth saying outright.
  const verdictText = (rollup: ConnectorRollup): string => {
    const verdict = rollup.status;
    if (
      verdict === 'fail' &&
      rollup.total > 0 &&
      (rollup.counts.connected ?? 0) === 0
    ) {
      return t('settingsServer.verdict.failAll');
    }
    const key = VERDICT[verdict];

    return key === undefined ? verdict : t(key);
  };

  // A state at zero says nothing, so only the states that hold a printer are
  // named.
  const countsText = (rollup: ConnectorRollup): string =>
    formatList(
      Object.entries(rollup.counts)
        .filter(([, count]) => count > 0)
        .map(([state, count]) =>
          t('settingsServer.count', {
            count: formatNumber(count),
            state: stateText(state),
          }),
        ),
      'unit',
    );

  const readiness = $derived(source.readiness);
  const blocked = $derived(readiness?.blocked ?? []);
  // An optional instance that is not serving leaves the server ready, which
  // is why the count can read short while the badge reads ready.
  const idle = $derived(
    (readiness?.instances ?? []).filter(
      (instance) => !instance.serving && !instance.required,
    ),
  );
  const anyAway = $derived(
    source.connectors.some((rollup) => rollup.status !== 'pass'),
  );
  const readinessFault = $derived(
    source.readinessError === undefined
      ? undefined
      : describeFault(source.readinessError),
  );
  const connectorsFault = $derived(
    source.connectorsError === undefined
      ? undefined
      : describeFault(source.connectorsError),
  );
</script>

<SettingsPane
  class="w-full"
  title={t('settingsServer.health.title')}
  description={t('settingsServer.health.description')}
>
  <div class="flex flex-col gap-6 pb-6">
    {#if !source.loaded}
      <div aria-busy="true" aria-label={t('settingsServer.loading')}>
        <Skeleton class="h-10 w-full" />
      </div>
    {:else}
      <section class="flex flex-col gap-2">
        <h3 class="text-sm font-semibold">
          {t('settingsServer.readiness.heading')}
        </h3>
        <FaultAlert fault={readinessFault} />
        {#if readiness}
          <div class="flex flex-wrap items-center gap-2">
            <Badge variant={readiness.ready ? 'success' : 'destructive'}>
              {readiness.ready
                ? t('settingsServer.readiness.ready')
                : t('settingsServer.readiness.notReady')}
            </Badge>
            <span class="text-sm text-muted-foreground">
              {t('settingsServer.readiness.serving', {
                serving: readiness.serving,
                declared: readiness.declared,
              })}
            </span>
          </div>
          {#if idle.length > 0}
            <p class="text-sm text-muted-foreground">
              {t('settingsServer.readiness.optional')}
            </p>
            <ul class="flex flex-col gap-1">
              {#each idle as instance (instance.resource)}
                <li class="flex flex-wrap items-baseline gap-x-2 text-sm">
                  <span class="font-mono text-xs">{instance.resource}</span>
                  <span class="text-muted-foreground">{instance.state}</span>
                </li>
              {/each}
            </ul>
          {/if}
          {#if blocked.length > 0}
            <p class="text-sm">{t('settingsServer.readiness.blocked')}</p>
            <ul class="flex flex-col gap-1">
              {#each blocked as resource (resource)}
                <li class="font-mono text-xs">{resource}</li>
              {/each}
            </ul>
          {/if}
        {/if}
      </section>

      <section class="flex flex-col gap-3">
        <h3 class="text-sm font-semibold">
          {t('settingsServer.connectors.heading')}
        </h3>
        <FaultAlert fault={connectorsFault} />
        {#if anyAway}
          <p class="text-sm text-muted-foreground">
            {t('settingsServer.connectors.away')}
          </p>
        {/if}
        {#if connectorsFault === undefined && source.connectors.length === 0}
          <p class="text-sm text-muted-foreground">
            {t('settingsServer.connectors.empty')}
          </p>
        {/if}
        {#each source.connectors as rollup (rollup.connector)}
          <Card>
            <CardContent class="flex flex-col gap-2">
              <div class="flex flex-wrap items-center gap-2">
                <span class="font-mono text-sm">{rollup.connector}</span>
                <Badge variant={VERDICT_VARIANT[rollup.status] ?? 'secondary'}>
                  {verdictText(rollup)}
                </Badge>
              </div>
              <p class="flex flex-wrap gap-x-3 text-sm text-muted-foreground">
                <span>
                  {t('settingsServer.connectors.total', {
                    count: rollup.total,
                  })}
                </span>
                {#if rollup.total > 0}
                  <span>{countsText(rollup)}</span>
                {/if}
              </p>
              {#if (rollup.degraded ?? []).length > 0}
                <ul class="flex flex-col gap-1 text-sm">
                  {#each rollup.degraded ?? [] as device (device.id)}
                    <li class="flex flex-wrap items-baseline gap-x-2">
                      <Button variant="link">
                        {#snippet child({ props })}
                          <a {...props} href={printerHref(device.id)}>
                            {#if device.name === undefined}
                              <span class="font-mono text-xs">{device.id}</span>
                            {:else}
                              {device.name}
                            {/if}
                          </a>
                        {/snippet}
                      </Button>
                      <span>{stateText(device.state)}</span>
                      {#if device.last_seen}
                        <span class="text-xs text-muted-foreground">
                          {t('settingsServer.device.lastSeen', {
                            at: formatDateTime(device.last_seen),
                          })}
                        </span>
                      {/if}
                      {#if device.since}
                        <span class="text-xs text-muted-foreground">
                          {t('settingsServer.device.since', {
                            at: formatDateTime(device.since),
                          })}
                        </span>
                      {/if}
                      {#if device.reason}
                        <span class="font-mono text-xs text-muted-foreground"
                          >{device.reason}</span
                        >
                      {/if}
                      {#if device.attempts > 0}
                        <span class="text-xs text-muted-foreground">
                          {t('settingsServer.device.attempts', {
                            count: device.attempts,
                          })}
                        </span>
                      {/if}
                    </li>
                  {/each}
                </ul>
                {#if rollup.degraded_truncated > 0}
                  <p class="text-xs text-muted-foreground">
                    {t('settingsServer.connectors.more', {
                      count: rollup.degraded_truncated,
                    })}
                  </p>
                {/if}
              {/if}
            </CardContent>
          </Card>
        {/each}
      </section>
    {/if}
  </div>
</SettingsPane>
