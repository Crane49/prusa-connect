<!--
  The Overview tab of the Server room: one card per part of the server, each
  saying in a line how that part stands and leading to the tab that says more,
  where there is one.
  Every card reads a store the other tabs read, so the overview never
  disagrees with the tab it leads to.
-->
<script lang="ts">
  import { PAIRING_NEEDED_FAULT } from '../../data';
  import { formatDateTime, formatNumber, t } from '../../i18n';
  import {
    Badge,
    type BadgeVariant,
    SettingsPane,
    Skeleton,
    Tile,
  } from '../../primitives';
  import { networkVerdict } from '../settings-network/verdict';
  import type { ServerOverviewProps } from './types';

  let {
    health,
    maintenance,
    network,
    healthHref,
    updatesHref,
    backupsHref,
    networkHref,
  }: ServerOverviewProps = $props();

  // An upgrade in one of these states has ended, so the card speaks of the
  // releases instead.
  const ENDED = new Set(['committed', 'rolled-back', 'failed-before-swap']);

  // Every key is written out rather than assembled from a value.
  const NETWORK: Readonly<Record<string, string>> = {
    announcing: 'settingsServer.overview.network.announcing',
    bridged: 'settingsServer.overview.network.bridged',
    hidden: 'settingsServer.overview.network.hidden',
    off: 'settingsServer.overview.network.off',
    failed: 'settingsServer.overview.network.failed',
  };
  const NETWORK_VARIANT: Readonly<Record<string, BadgeVariant>> = {
    announcing: 'success',
    bridged: 'warning',
    hidden: 'secondary',
    off: 'secondary',
    failed: 'destructive',
  };

  const readiness = $derived(health.readiness);
  const statusVariant = $derived.by((): BadgeVariant => {
    if (readiness?.ready === true) return 'success';
    if (readiness?.ready === false) return 'destructive';

    return 'secondary';
  });
  const statusText = $derived.by((): string => {
    if (readiness?.ready === true) return t('settingsServer.summary.healthy');
    if (readiness?.ready === false)
      return t('settingsServer.summary.attention');

    return t('settingsServer.summary.unknown');
  });

  const printers = $derived.by(() => {
    let total = 0;
    let connected = 0;
    for (const rollup of health.connectors) {
      total += rollup.total;
      connected += rollup.counts.connected ?? 0;
    }

    return { total, connected, away: total - connected };
  });

  const upgrading = $derived(
    maintenance.upgrade !== undefined && !ENDED.has(maintenance.upgrade.state)
      ? maintenance.upgrade
      : undefined,
  );
  const newest = $derived(maintenance.releases[0]);
  const lastBackup = $derived(maintenance.backups[0]);

  const announcement = $derived(
    networkVerdict(network.announcement, network.described?.bridged),
  );
  const networkText = $derived.by(() => {
    const key = announcement === undefined ? undefined : NETWORK[announcement];

    return key === undefined ? undefined : t(key);
  });
  const announcedAt = $derived.by(() => {
    const described = network.described;
    if (described?.host === undefined) return undefined;

    return described.port === undefined
      ? described.host
      : `${described.host}:${String(described.port)}`;
  });
</script>

<SettingsPane
  class="w-full"
  title={t('settingsServer.overview.title')}
  description={t('settingsServer.overview.description')}
>
  <div class="@container/overview pb-6">
    <ul class="grid gap-3 @lg/overview:grid-cols-2">
      <li>
        <Tile href={healthHref} class="min-w-0" data-part="status">
          <span class="text-caption text-muted-foreground">
            {t('settingsServer.overview.status')}
          </span>
          {#if health.loaded}
            <span class="flex flex-wrap items-center gap-2">
              <Badge variant={statusVariant}>{statusText}</Badge>
              {#if readiness !== undefined && readiness.serving < readiness.declared}
                <span class="text-sm text-muted-foreground">
                  {t('settingsServer.readiness.serving', {
                    serving: readiness.serving,
                    declared: readiness.declared,
                  })}
                </span>
              {/if}
            </span>
          {:else}
            <Skeleton class="h-6 w-24" />
          {/if}
        </Tile>
      </li>

      <li>
        <Tile href={healthHref} class="min-w-0" data-part="printers">
          <span class="text-caption text-muted-foreground">
            {t('settingsServer.overview.printers')}
          </span>
          {#if !health.loaded}
            <Skeleton class="h-6 w-32" />
          {:else if printers.total === 0}
            <span class="text-sm"
              >{t('settingsServer.overview.noPrinters')}</span
            >
          {:else}
            <span class="flex flex-wrap items-center gap-2">
              <span class="text-sm font-medium">
                {t('settingsServer.overview.connected', {
                  connected: formatNumber(printers.connected),
                  total: formatNumber(printers.total),
                })}
              </span>
              {#if printers.away > 0}
                <Badge variant="warning">
                  {t('settingsServer.overview.away', { count: printers.away })}
                </Badge>
              {/if}
            </span>
          {/if}
        </Tile>
      </li>

      <li>
        <!-- A server that prepares no upgrade has no Updates tab, so the tile
             names the version and is not a link. -->
        <Tile href={updatesHref} class="min-w-0" data-part="version">
          <span class="text-caption text-muted-foreground">
            {t('settingsServer.summary.version')}
          </span>
          {#if maintenance.loaded}
            <span class="flex flex-wrap items-center gap-2">
              <span class="font-mono text-sm font-medium">
                {maintenance.version?.version ??
                  t('settingsServer.summary.unavailable')}
              </span>
              {#if updatesHref !== undefined}
                {#if upgrading !== undefined}
                  <Badge variant="info">
                    {t('settingsServer.overview.upgrading', {
                      state: upgrading.state,
                    })}
                  </Badge>
                {:else if newest !== undefined}
                  <Badge variant="info">
                    {t('settingsServer.summary.available', {
                      version: newest.version,
                    })}
                  </Badge>
                {:else if maintenance.releaseLoading}
                  <!-- Nothing is claimed about newer releases before the list is read. -->
                {:else if maintenance.releaseError !== undefined}
                  <span class="text-sm text-muted-foreground">
                    {maintenance.releaseError.code === PAIRING_NEEDED_FAULT
                      ? t('settingsServer.summary.unpaired')
                      : t('settingsServer.summary.unchecked')}
                  </span>
                {:else}
                  <span class="text-sm text-muted-foreground">
                    {t('settingsServer.summary.current')}
                  </span>
                {/if}
              {/if}
            </span>
            {#if updatesHref === undefined}
              <span class="text-sm text-muted-foreground">
                {t('settingsServer.overview.updatesOffNote')}
              </span>
            {/if}
          {:else}
            <Skeleton class="h-6 w-32" />
          {/if}
        </Tile>
      </li>

      {#if backupsHref !== undefined}
        <li>
          <Tile href={backupsHref} class="min-w-0" data-part="backup">
            <span class="text-caption text-muted-foreground">
              {t('settingsServer.summary.backup')}
            </span>
            {#if maintenance.loaded}
              <span class="text-sm font-medium">
                {lastBackup === undefined
                  ? t('settingsServer.summary.noBackup')
                  : formatDateTime(lastBackup.created_at)}
              </span>
            {:else}
              <Skeleton class="h-6 w-32" />
            {/if}
          </Tile>
        </li>
      {/if}

      <li>
        <Tile href={networkHref} class="min-w-0" data-part="network">
          <span class="text-caption text-muted-foreground">
            {t('settingsServer.overview.network')}
          </span>
          {#if network.loaded}
            <span class="flex flex-wrap items-center gap-2">
              {#if networkText !== undefined && announcement !== undefined}
                <Badge variant={NETWORK_VARIANT[announcement] ?? 'secondary'}>
                  {networkText}
                </Badge>
              {/if}
              {#if announcement === 'announcing' && announcedAt !== undefined}
                <span class="truncate font-mono text-sm">{announcedAt}</span>
              {/if}
            </span>
            {#if announcement === 'off'}
              <span class="text-sm text-muted-foreground">
                {t('settingsServer.overview.network.offNote')}
              </span>
            {/if}
          {:else if network.error !== undefined}
            <span class="text-sm text-muted-foreground">
              {t('settingsServer.summary.unavailable')}
            </span>
          {:else}
            <Skeleton class="h-6 w-32" />
          {/if}
        </Tile>
      </li>
    </ul>
  </div>
</SettingsPane>
