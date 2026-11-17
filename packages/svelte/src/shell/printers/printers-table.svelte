<!--
  Every printer as a row, for a container wide enough to hold eight columns.
  The narrow tier beside it draws the same facts stacked.
-->
<script lang="ts">
  import { PRINTER_STATE_LABEL_KEY } from '../../domain';
  import { t } from '../../i18n';
  import {
    Badge,
    ClippedName,
    HeatbedIcon,
    NozzleIcon,
    PrinterIcon,
    Table,
    TableBody,
    TableCaption,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
    TemperatureReading,
  } from '../../primitives';
  import { stateBadgeVariant } from './presentation';
  import type { PrinterFieldId, PrinterRow } from './types';

  let {
    rows,
    shows,
  }: {
    rows: readonly PrinterRow[];
    shows: (field: PrinterFieldId) => boolean;
  } = $props();
</script>

{#snippet nozzleIcon()}
  <NozzleIcon aria-hidden="true" class="size-3.5" />
{/snippet}

{#snippet bedIcon()}
  <HeatbedIcon aria-hidden="true" class="size-3.5" />
{/snippet}

<Table>
  <TableCaption class="sr-only">{t('printers.caption')}</TableCaption>
  <TableHeader>
    <TableRow>
      <TableHead>{t('printers.printer')}</TableHead>
      {#if shows('state')}<TableHead>{t('printers.state')}</TableHead>{/if}
      {#if shows('job')}<TableHead>{t('printers.job')}</TableHead>{/if}
      {#if shows('progress')}<TableHead>{t('printers.progress')}</TableHead
        >{/if}
      {#if shows('nozzle')}<TableHead>{t('printers.nozzle')}</TableHead>{/if}
      {#if shows('bed')}<TableHead>{t('printers.bed')}</TableHead>{/if}
      {#if shows('material')}<TableHead>{t('printers.material')}</TableHead
        >{/if}
      <TableHead class="sr-only">{t('printers.actions')}</TableHead>
    </TableRow>
  </TableHeader>
  <TableBody>
    {#each rows as row (row.id)}
      <TableRow
        class={row.tier === 'critical' ? 'bg-destructive/5' : undefined}
      >
        <TableCell class="max-w-64">
          <div class="flex items-center gap-3">
            <PrinterIcon
              model={row.artwork}
              reported={row.model}
              class="size-11 shrink-0"
            />
            <div class="min-w-0">
              <a
                href={row.detailHref}
                class="block truncate rounded-sm focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background focus-visible:outline-none"
                >{row.name}</a
              >
              {#if row.model !== row.name}
                <p class="truncate text-xs text-muted-foreground">
                  {row.model}
                </p>
              {/if}
            </div>
          </div>
        </TableCell>
        {#if shows('state')}
          <TableCell>
            {#if row.retired}
              <Badge mono variant="secondary">
                {t('printers.retired')}
              </Badge>
            {:else}
              <Badge variant={stateBadgeVariant(row.state)}>
                {t(PRINTER_STATE_LABEL_KEY[row.state])}
              </Badge>
            {/if}
          </TableCell>
        {/if}
        {#if shows('job')}
          <TableCell class="max-w-48">
            {#if row.jobName === undefined}
              <span class="text-muted-foreground">{t('printers.noJob')}</span>
            {:else}
              <ClippedName name={row.jobName} />
            {/if}
          </TableCell>
        {/if}
        {#if shows('progress')}
          <TableCell>
            {#if row.progress !== null}{row.progress}%{/if}
          </TableCell>
        {/if}
        {#if shows('nozzle')}
          <TableCell>
            <TemperatureReading
              icon={nozzleIcon}
              current={row.nozzle}
              target={row.nozzleTarget}
            />
          </TableCell>
        {/if}
        {#if shows('bed')}
          <TableCell>
            <TemperatureReading
              icon={bedIcon}
              current={row.bed}
              target={row.bedTarget}
            />
          </TableCell>
        {/if}
        {#if shows('material')}
          <TableCell>{row.material ?? ''}</TableCell>
        {/if}
        <TableCell></TableCell>
      </TableRow>
    {/each}
  </TableBody>
</Table>
