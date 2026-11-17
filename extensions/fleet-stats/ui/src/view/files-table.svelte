<!--
  The files printed in the period, most printed first: how often, how many
  failed or were cancelled, and the printing time all their prints took.
-->
<script lang="ts">
  import {
    Button,
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
  } from '@prusa/connect-ui/primitives';
  import { formatDateTime, formatNumber } from '@prusa/connect-ui/i18n';

  import type { FileRow } from '../domain/job-stats';
  import { heldHost } from '../host';
  import { durationOf, rateOf } from './metrics';

  interface Props {
    files: readonly FileRow[];
    /** How many rows show before a person asks for all of them. */
    limit?: number;
  }

  let { files, limit = 10 }: Props = $props();
  const t = heldHost().t;

  let everything = $state(false);
  const shown = $derived(everything ? files : files.slice(0, limit));
</script>

<div class="flex flex-col gap-2" data-part="fleet-stats-files">
  <Table>
    <TableHeader>
      <TableRow>
        <TableHead>{t('fleet-stats.files.name')}</TableHead>
        <TableHead class="text-end">{t('fleet-stats.files.prints')}</TableHead>
        <TableHead class="text-end">{t('fleet-stats.files.success')}</TableHead>
        <TableHead class="text-end">{t('fleet-stats.files.failed')}</TableHead>
        <TableHead class="text-end">{t('fleet-stats.files.time')}</TableHead>
        <TableHead class="text-end">{t('fleet-stats.files.last')}</TableHead>
      </TableRow>
    </TableHeader>
    <TableBody>
      {#each shown as row (row.name)}
        <TableRow>
          <TableCell class="max-w-64 truncate" title={row.name}
            >{row.name === ''
              ? t('fleet-stats.files.unnamed')
              : row.name}</TableCell
          >
          <TableCell class="text-end">{formatNumber(row.prints)}</TableCell>
          <TableCell class="text-end">{rateOf(t, row.successRate)}</TableCell>
          <TableCell class="text-end"
            >{formatNumber(row.failed + row.cancelled)}</TableCell
          >
          <TableCell class="text-end">{durationOf(t, row.printMs)}</TableCell>
          <TableCell class="text-end text-muted-foreground"
            >{formatDateTime(row.lastEnded)}</TableCell
          >
        </TableRow>
      {/each}
    </TableBody>
  </Table>
  {#if files.length > limit}
    <Button
      variant="ghost"
      size="sm"
      class="self-start"
      onclick={() => (everything = !everything)}
    >
      {everything
        ? t('fleet-stats.files.fewer')
        : t('fleet-stats.files.all', { count: files.length })}
    </Button>
  {/if}
</div>
