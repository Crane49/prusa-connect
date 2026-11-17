<!--
  Every file this server sent to the printer that it still knows about,
  newest first: which file, how it ended, how many bytes reached the printer
  and, for a send that did not finish, the reason it stopped.
-->
<script lang="ts">
  import InboxIcon from '@lucide/svelte/icons/inbox';

  import type { TransferRow } from '../../../data';
  import { describeFault, formatBytes, formatDateTime, t } from '../../../i18n';
  import {
    Badge,
    type BadgeVariant,
    Card,
    CardContent,
    CardHeader,
    CardTitle,
    ClippedName,
    EmptyState,
    FaultAlert,
    ListingSkeleton,
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
  } from '../../../primitives';
  import type { TransferLogProps } from './types';

  let { source }: TransferLogProps = $props();

  /** Every key is written out rather than assembled from a value. */
  const STATE_KEY: Readonly<Record<string, string>> = {
    cancelled: 'printerDetailHistory.transfers.state.cancelled',
    done: 'printerDetailHistory.transfers.state.done',
    failed: 'printerDetailHistory.transfers.state.failed',
    interrupted: 'printerDetailHistory.transfers.state.interrupted',
    queued: 'printerDetailHistory.transfers.state.queued',
    sending: 'printerDetailHistory.transfers.state.sending',
  };

  const STATE_VARIANT: Readonly<Record<string, BadgeVariant>> = {
    cancelled: 'warning',
    done: 'success',
    failed: 'destructive',
    interrupted: 'warning',
    queued: 'secondary',
    sending: 'info',
  };

  const fault = $derived(
    source.error === undefined ? undefined : describeFault(source.error),
  );

  const nameOf = (row: TransferRow): string =>
    row.path.split('/').pop() ?? row.path;

  const whenOf = (row: TransferRow): string => {
    const at = row.startedAt ?? row.endedAt;

    return at === undefined ? '' : formatDateTime(at);
  };

  // A state a later build adds is drawn as the server spelled it.
  const stateText = (row: TransferRow): string => {
    const key = STATE_KEY[row.state];

    return key === undefined ? row.state : t(key);
  };

  const sentText = (row: TransferRow): string =>
    row.bytesTotal > 0
      ? t('printerDetailHistory.transfers.sentOf', {
          sent: formatBytes(row.bytesSent),
          total: formatBytes(row.bytesTotal),
        })
      : formatBytes(row.bytesSent);
</script>

<Card>
  <CardHeader>
    <CardTitle>
      <h2 class="contents">{t('printerDetailHistory.transfers.heading')}</h2>
    </CardTitle>
  </CardHeader>
  <CardContent class="flex flex-col gap-4">
    <!-- A failed read keeps the rows of an earlier one. -->
    {#if fault}
      <FaultAlert {fault} />
    {/if}
    {#if !source.loaded && fault === undefined}
      <ListingSkeleton
        label={t('printerDetailHistory.transfers.loading')}
        rows={3}
      />
    {:else if source.rows.length === 0 && fault === undefined}
      <EmptyState
        icon={InboxIcon}
        heading={t('printerDetailHistory.transfers.empty.heading')}
        headingLevel="h3"
      />
    {:else if source.rows.length > 0}
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead
              >{t('printerDetailHistory.transfers.column.time')}</TableHead
            >
            <TableHead
              >{t('printerDetailHistory.transfers.column.file')}</TableHead
            >
            <TableHead
              >{t('printerDetailHistory.transfers.column.state')}</TableHead
            >
            <TableHead class="text-end"
              >{t('printerDetailHistory.transfers.column.sent')}</TableHead
            >
            <TableHead
              >{t('printerDetailHistory.transfers.column.reason')}</TableHead
            >
          </TableRow>
        </TableHeader>
        <TableBody>
          {#each source.rows as row (row.transferId)}
            <TableRow>
              <TableCell class="text-xs tabular-nums">{whenOf(row)}</TableCell>
              <TableCell class="max-w-64">
                <ClippedName
                  name={nameOf(row)}
                  class="min-w-0 font-mono text-sm"
                />
              </TableCell>
              <TableCell>
                <Badge variant={STATE_VARIANT[row.state] ?? 'secondary'}>
                  {stateText(row)}
                </Badge>
              </TableCell>
              <TableCell class="text-end text-xs tabular-nums">
                {sentText(row)}
              </TableCell>
              <TableCell class="text-sm whitespace-normal">
                {row.reason ?? ''}
              </TableCell>
            </TableRow>
          {/each}
        </TableBody>
      </Table>
    {/if}
  </CardContent>
</Card>
