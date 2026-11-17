<!-- Where one job is: a badge, and while it slices, how far it is. -->
<script lang="ts">
  import {
    Badge,
    type BadgeVariant,
    Progress,
  } from '@prusa/connect-ui/primitives';

  import type { Job, Translate } from '../../api';
  import { projectOf } from '../../jobs';

  let { job, t }: { job: Job; t: Translate } = $props();

  const variants: Record<Job['state'], BadgeVariant> = {
    queued: 'secondary',
    running: 'info',
    done: 'success',
    failed: 'destructive',
    canceled: 'secondary',
  };
</script>

<div class="grid min-w-0 grid-cols-1 content-start gap-1" data-part="job-state">
  <Badge variant={variants[job.state]} class="w-fit"
    >{t(`slicer.state.${job.state}`)}</Badge
  >
  {#if job.state === 'running'}
    <Progress
      value={job.progress >= 0 ? job.progress : null}
      aria-label={t('slicer.jobs.progress', { name: projectOf(job) })}
      class="w-24"
    />
    {#if job.step}
      <span class="truncate text-xs text-muted-foreground">{job.step}</span>
    {/if}
  {/if}
</div>
