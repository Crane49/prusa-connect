<!--
  The same facts as the table, stacked, for a container too narrow to hold
  eight columns without wrapping every cell.
-->
<script lang="ts">
  import { PRINTER_STATE_LABEL_KEY } from '../../domain';
  import { formatUnit, t } from '../../i18n';
  import {
    DataRow,
    PrinterIcon,
    RowList,
    type DataRowValue,
  } from '../../primitives';
  import type { PrinterFieldId, PrinterRow } from './types';

  let {
    rows,
    shows,
  }: {
    rows: readonly PrinterRow[];
    shows: (field: PrinterFieldId) => boolean;
  } = $props();

  const stateOf = (row: PrinterRow): string | undefined => {
    if (row.retired) return t('printers.retired');

    return shows('state') ? t(PRINTER_STATE_LABEL_KEY[row.state]) : undefined;
  };

  const secondaries = (row: PrinterRow): DataRowValue[] => [
    stateOf(row),
    shows('job') ? (row.jobName ?? t('printers.noJob')) : undefined,
    shows('progress') && row.progress !== null
      ? formatUnit(row.progress, '%')
      : undefined,
    shows('material') ? row.material : undefined,
  ];
</script>

<RowList>
  {#each rows as row (row.id)}
    <DataRow
      href={row.detailHref}
      primary={row.name}
      secondaryValues={secondaries(row)}
      class="min-h-18"
    >
      {#snippet leading()}
        <PrinterIcon
          model={row.artwork}
          reported={row.model}
          class="size-14 shrink-0"
        />
      {/snippet}
    </DataRow>
  {/each}
</RowList>
