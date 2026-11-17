<!--
  Every MAC address that sent something, and the printer it is matched to. A
  person matches one by hand where the module cannot, such as a printer this
  server has not seen connect.
-->
<script lang="ts">
  import { formatDateTime, formatNumber } from '@prusa/connect-ui/i18n';
  import {
    Badge,
    EmptyState,
    FaultAlert,
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
    RowList,
  } from '@prusa/connect-ui/primitives';
  import RadioTowerIcon from '@lucide/svelte/icons/radio-tower';

  import type { Source } from '../api';
  import { faultText } from '../faults';
  import { heldSession } from '../session';

  const { host, status } = heldSession();
  const t = host.t;

  const AUTOMATIC = 'automatic';

  let refused = $state<unknown>(undefined);
  const fault = $derived(
    refused === undefined ? undefined : faultText(refused, t),
  );

  const choices = $derived([
    { value: AUTOMATIC, label: t('buddyDebugLogs.sources.automatic') },
    ...host.fleet.devices.map((one) => ({
      value: one.id,
      label: one.name || one.id,
    })),
  ]);

  const valueOf = (source: Source): string =>
    source.manual && source.device_id ? source.device_id : AUTOMATIC;

  const assign = async (source: Source, next: string): Promise<void> => {
    refused = undefined;
    try {
      await status.assign(source.mac, next === AUTOMATIC ? '' : next);
    } catch (thrown) {
      refused = thrown;
    }
  };

  const matchedName = (source: Source): string | undefined =>
    source.device_id
      ? (host.fleet.device(source.device_id)?.name ??
        source.device_name ??
        source.device_id)
      : undefined;
</script>

<div class="grid gap-3" data-part="sources-view">
  <p class="text-sm text-muted-foreground">
    {t('buddyDebugLogs.sources.note')}
  </p>
  <p class="text-sm text-muted-foreground" data-part="sources-unverified">
    {t('buddyDebugLogs.sources.unverified')}
  </p>
  <FaultAlert {fault} />
  {#if status.sources?.length === 0}
    <EmptyState
      icon={RadioTowerIcon}
      headingLevel="h3"
      heading={t('buddyDebugLogs.sources.empty.heading')}
      description={t('buddyDebugLogs.sources.empty.note')}
      data-part="sources-empty"
    />
  {:else if status.sources !== undefined}
    <RowList class="overflow-x-auto">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('buddyDebugLogs.sources.mac')}</TableHead>
            <TableHead>{t('buddyDebugLogs.sources.matched')}</TableHead>
            <TableHead>{t('buddyDebugLogs.sources.address')}</TableHead>
            <TableHead>{t('buddyDebugLogs.sources.lastSeen')}</TableHead>
            <TableHead>{t('buddyDebugLogs.sources.counts')}</TableHead>
            <TableHead>{t('buddyDebugLogs.sources.assign')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {#each status.sources as source (source.mac)}
            {@const name = matchedName(source)}
            <TableRow data-part="source">
              <TableCell class="font-mono text-xs">{source.mac}</TableCell>
              <TableCell class="text-sm">
                {#if name}
                  {name}
                  {#if source.manual}
                    <Badge variant="outline"
                      >{t('buddyDebugLogs.sources.manual')}</Badge
                    >
                  {/if}
                {:else}
                  <Badge variant="warning"
                    >{t('buddyDebugLogs.logs.unmatchedSender')}</Badge
                  >
                {/if}
              </TableCell>
              <TableCell class="font-mono text-xs"
                >{source.last_ip ?? ''}</TableCell
              >
              <TableCell class="text-xs whitespace-nowrap"
                >{formatDateTime(source.last_seen)}</TableCell
              >
              <TableCell class="text-xs">
                {t('buddyDebugLogs.sources.countsValue', {
                  syslog: formatNumber(source.syslog),
                  metrics: formatNumber(source.metrics),
                })}
              </TableCell>
              <TableCell>
                <Select
                  value={valueOf(source)}
                  items={choices}
                  onValueChange={(next: string) => {
                    void assign(source, next);
                  }}
                >
                  <SelectTrigger
                    size="sm"
                    class="min-w-40"
                    aria-label={t('buddyDebugLogs.sources.assign')}
                  >
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {#each choices as choice (choice.value)}
                      <SelectItem value={choice.value} label={choice.label}>
                        {choice.label}
                      </SelectItem>
                    {/each}
                  </SelectContent>
                </Select>
              </TableCell>
            </TableRow>
          {/each}
        </TableBody>
      </Table>
    </RowList>
  {/if}
</div>
