<script lang="ts">
  import FileCode2Icon from '@lucide/svelte/icons/file-code-2';

  import { PRINTER_STATE_LABEL_KEY } from '../../domain';
  import { formatDateTime, formatDuration, formatUnit, t } from '../../i18n';
  import {
    ABOVE_ROW_LINK,
    Badge,
    ClippedName,
    FileThumbnail,
    LINKED_ROW,
    Progress,
    ROW_LINK,
    SortableTableHead,
    Table,
    TableBody,
    TableCaption,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
    THUMBNAIL_ROW_MIN_HEIGHT,
    PrinterGlyphIcon,
  } from '../../primitives';
  import { stateBadgeVariant } from '../printers/presentation';
  import JobsRowActions from './jobs-row-actions.svelte';
  import { progressTone } from './presentation';
  import type { JobFieldId, JobRow, JobSort, JobSortColumn } from './types';

  let {
    rows,
    shows,
    sort,
    onSort,
    printAgainOf,
    thumbnailFor,
  }: {
    rows: readonly JobRow[];
    shows: (field: JobFieldId) => boolean;
    sort: JobSort;
    onSort: (column: JobSortColumn) => void;
    /** The Print again press for one row, or undefined where the row offers none. */
    printAgainOf?: ((row: JobRow) => (() => void) | undefined) | undefined;
    thumbnailFor?: ((row: JobRow) => string | undefined) | undefined;
  } = $props();

  const directionOf = (
    column: JobSortColumn,
  ): 'ascending' | 'descending' | undefined => {
    if (sort.column !== column) return undefined;

    return sort.ascending ? 'ascending' : 'descending';
  };
</script>

<Table>
  <TableCaption class="sr-only">{t('jobs.caption')}</TableCaption>
  <TableHeader>
    <TableRow>
      {#if shows('preview')}
        <TableHead class="sr-only">{t('jobs.preview')}</TableHead>
      {/if}
      <TableHead>{t('jobs.file')}</TableHead>
      {#if shows('printer')}<TableHead>{t('jobs.printer')}</TableHead>{/if}
      {#if shows('state')}<TableHead>{t('jobs.state')}</TableHead>{/if}
      {#if shows('started')}
        <SortableTableHead
          label={t('jobs.started')}
          direction={directionOf('started')}
          onSort={() => {
            onSort('started');
          }}
        />
      {/if}
      {#if shows('duration')}
        <SortableTableHead
          label={t('jobs.duration')}
          direction={directionOf('duration')}
          align="end"
          onSort={() => {
            onSort('duration');
          }}
        />
      {/if}
      {#if shows('progress')}<TableHead>{t('jobs.progress')}</TableHead>{/if}
      <!-- The menu acts on the row the File column names. -->
      <TableHead class="sr-only">{t('jobs.actions')}</TableHead>
    </TableRow>
  </TableHeader>
  <TableBody>
    {#each rows as row (row.id)}
      <TableRow class={[THUMBNAIL_ROW_MIN_HEIGHT, LINKED_ROW]}>
        {#if shows('preview')}
          <TableCell>
            <FileThumbnail
              name={row.fileName}
              glyph={FileCode2Icon}
              thumbnailUrl={thumbnailFor?.(row)}
            />
          </TableCell>
        {/if}
        <TableCell class="max-w-72">
          <a href={row.detailHref} class={['block rounded-sm', ROW_LINK]}>
            <ClippedName name={row.fileName} class="font-mono text-sm" />
          </a>
        </TableCell>
        {#if shows('printer')}
          <TableCell class="max-w-40 overflow-hidden">
            <!-- Lifted over the row's own overlay. -->
            <a
              href={row.printerHref}
              title={row.printerName}
              class={[
                'flex max-w-full min-w-0 items-center gap-1.5 rounded-sm underline underline-offset-4 hover:text-link focus-visible:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background focus-visible:outline-none',
                ABOVE_ROW_LINK,
              ]}
            >
              <PrinterGlyphIcon aria-hidden="true" class="size-3 shrink-0" />
              <span class="min-w-0 truncate">{row.printerName}</span>
            </a>
          </TableCell>
        {/if}
        {#if shows('state')}
          <TableCell>
            <Badge variant={stateBadgeVariant(row.state)}>
              {t(PRINTER_STATE_LABEL_KEY[row.state])}
            </Badge>
          </TableCell>
        {/if}
        {#if shows('started')}
          <TableCell class="whitespace-nowrap tabular-nums">
            {formatDateTime(row.started)}
          </TableCell>
        {/if}
        {#if shows('duration')}
          <TableCell class="text-end whitespace-nowrap tabular-nums">
            {row.durationSeconds === undefined
              ? ''
              : formatDuration(row.durationSeconds)}
          </TableCell>
        {/if}
        {#if shows('progress')}
          <TableCell class="w-28">
            <div class="flex items-center gap-2">
              <Progress
                value={row.progress}
                tone={progressTone(row)}
                aria-label={t('jobs.progressFor', { name: row.fileName })}
                class="flex-1"
              />
              <span class="shrink-0 tabular-nums">
                {formatUnit(row.progress, '%')}
              </span>
            </div>
          </TableCell>
        {/if}
        <!-- Lifted over the row's own overlay. -->
        <TableCell class={ABOVE_ROW_LINK}>
          <JobsRowActions {row} {printAgainOf} />
        </TableCell>
      </TableRow>
    {/each}
  </TableBody>
</Table>
