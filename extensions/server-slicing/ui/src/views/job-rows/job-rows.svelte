<!-- The same facts as the table, one row each, for a narrow screen. -->
<script lang="ts">
  import { formatDateTime, formatDuration } from '@prusa/connect-ui/i18n';
  import { DataRow, RowList } from '@prusa/connect-ui/primitives';

  import type { Job, Translate } from '../../api';
  import { durationOf, outputOf, projectOf, slicerOf } from '../../jobs';
  import { JobActions } from '../job-actions';
  import { JobState } from '../job-state';

  let {
    jobs,
    all,
    shows,
    hrefOf,
    onCancel,
    t,
  }: {
    jobs: readonly Job[];
    all: boolean;
    shows: (field: string) => boolean;
    hrefOf: (job: Job) => string;
    onCancel: (id: string) => void;
    t: Translate;
  } = $props();
</script>

<RowList data-part="job-rows">
  {#each jobs as job (job.id)}
    {@const took = durationOf(job)}
    {#snippet state()}<JobState {job} {t} />{/snippet}
    <DataRow
      href={hrefOf(job)}
      primary={projectOf(job)}
      mono
      wrapValues
      data-job={job.id}
      secondaryValues={[
        shows('state') ? state : undefined,
        shows('slicer') ? slicerOf(job) : undefined,
        shows('created') ? formatDateTime(job.created_at) : undefined,
        shows('duration') && took !== undefined
          ? formatDuration(took)
          : undefined,
        shows('output') ? outputOf(job) : undefined,
        all && shows('by') ? job.by : undefined,
      ]}
    >
      {#snippet actions()}
        <JobActions {job} detailHref={hrefOf(job)} {onCancel} {t} />
      {/snippet}
    </DataRow>
  {/each}
</RowList>
