<!--
  The jobs as a table. The project names the row and opens the job's detail;
  the menu at the end acts on it. Columns the container has no room for fold
  away, so the page never scrolls sideways.
-->
<script lang="ts">
  import { formatDateTime, formatDuration } from '@prusa/connect-ui/i18n';
  import {
    ABOVE_ROW_LINK,
    LINKED_ROW,
    ROW_LINK,
    Table,
    TableBody,
    TableCaption,
    TableCell,
    TableHead,
    TableHeader,
    SortableTableHead,
    TableRow,
  } from '@prusa/connect-ui/primitives';
  import FileCodeIcon from '@lucide/svelte/icons/file-code';

  import type { Job, Translate } from '../../api';
  import {
    durationOf,
    fileHref,
    type JobSort,
    type JobSortColumn,
    outputOf,
    projectOf,
    slicerOf,
  } from '../../jobs';
  import { JobActions } from '../job-actions';
  import { JobState } from '../job-state';

  let {
    jobs,
    all,
    shows,
    sort,
    onSort,
    hrefOf,
    onCancel,
    t,
  }: {
    jobs: readonly Job[];
    /** Whether the list holds every person's jobs, so the person is named. */
    all: boolean;
    shows: (field: string) => boolean;
    sort: JobSort;
    onSort: (column: JobSortColumn) => void;
    hrefOf: (job: Job) => string;
    onCancel: (id: string) => void;
    t: Translate;
  } = $props();

  const directionOf = (
    column: JobSortColumn,
  ): 'ascending' | 'descending' | undefined => {
    if (sort.column !== column) return undefined;

    return sort.ascending ? 'ascending' : 'descending';
  };
</script>

{#snippet sortable(label: string, column: JobSortColumn, cls?: string)}
  <SortableTableHead
    {label}
    direction={directionOf(column)}
    onSort={() => onSort(column)}
    class={cls}
  />
{/snippet}

<Table data-part="job-table">
  <TableCaption class="sr-only">{t('slicer.jobs.caption')}</TableCaption>
  <TableHeader>
    <TableRow>
      <TableHead>{t('slicer.column.project')}</TableHead>
      {#if shows('state')}<TableHead>{t('slicer.column.state')}</TableHead>{/if}
      {#if shows('slicer')}
        <TableHead class="hidden @xl/jobs:table-cell"
          >{t('slicer.column.slicer')}</TableHead
        >
      {/if}
      {#if shows('created')}
        {@render sortable(
          t('slicer.column.created'),
          'created',
          'hidden @3xl/jobs:table-cell',
        )}
      {/if}
      {#if shows('duration')}
        {@render sortable(
          t('slicer.column.duration'),
          'duration',
          'hidden @2xl/jobs:table-cell',
        )}
      {/if}
      {#if shows('output')}
        <TableHead class="hidden @4xl/jobs:table-cell"
          >{t('slicer.column.result')}</TableHead
        >
      {/if}
      {#if all && shows('by')}
        <TableHead class="hidden @5xl/jobs:table-cell"
          >{t('slicer.column.by')}</TableHead
        >
      {/if}
      <TableHead class="w-0"
        ><span class="sr-only">{t('slicer.column.actions')}</span></TableHead
      >
    </TableRow>
  </TableHeader>
  <TableBody>
    {#each jobs as job (job.id)}
      {@const output = outputOf(job)}
      {@const took = durationOf(job)}
      <TableRow class={LINKED_ROW} data-part="job-row" data-job={job.id}>
        <TableCell class="max-w-64">
          <a
            href={hrefOf(job)}
            class={['block truncate rounded-sm font-medium', ROW_LINK]}
            title={projectOf(job)}>{projectOf(job)}</a
          >
          {#if job.error}
            <span class="block truncate text-xs text-destructive-text"
              >{t(`error.${job.error.code}`)}</span
            >
          {/if}
        </TableCell>
        {#if shows('state')}
          <TableCell class="min-w-28"><JobState {job} {t} /></TableCell>
        {/if}
        {#if shows('slicer')}
          <TableCell class="hidden whitespace-nowrap @xl/jobs:table-cell"
            >{slicerOf(job)}</TableCell
          >
        {/if}
        {#if shows('created')}
          <TableCell
            class="hidden whitespace-nowrap tabular-nums @3xl/jobs:table-cell"
            >{formatDateTime(job.created_at)}</TableCell
          >
        {/if}
        {#if shows('duration')}
          <TableCell
            class="hidden whitespace-nowrap tabular-nums @2xl/jobs:table-cell"
            >{took === undefined ? '' : formatDuration(took)}</TableCell
          >
        {/if}
        {#if shows('output')}
          <TableCell class="hidden max-w-56 @4xl/jobs:table-cell">
            {#if job.result && output}
              <a
                class={[
                  'inline-flex max-w-full items-center gap-1 text-primary underline-offset-4 hover:underline',
                  ABOVE_ROW_LINK,
                ]}
                href={fileHref(job.result.storage, job.result.path)}
              >
                <FileCodeIcon class="size-4 shrink-0" aria-hidden="true" />
                <span class="truncate">{output}</span>
              </a>
            {/if}
          </TableCell>
        {/if}
        {#if all && shows('by')}
          <TableCell class="hidden max-w-40 truncate @5xl/jobs:table-cell"
            >{job.by}</TableCell
          >
        {/if}
        <TableCell class={ABOVE_ROW_LINK}>
          <JobActions {job} detailHref={hrefOf(job)} {onCancel} {t} />
        </TableCell>
      </TableRow>
    {/each}
  </TableBody>
</Table>
