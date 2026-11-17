<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { someJobs, t } from '../../test/fakes';
  import JobState from './job-state.svelte';

  const { Story } = defineMeta({
    title: 'Server slicing/JobState',
    component: JobState,
    args: { t },
  });

  const jobOf = (id: string) => {
    const found = someJobs.jobs.find((job) => job.id === id);
    if (!found) throw new Error(`no fake job ${id}`);
    return found;
  };
</script>

<Story name="Slicing" args={{ job: jobOf('a1') }} />
<Story name="Waiting" args={{ job: jobOf('a2') }} />
<Story name="Done" args={{ job: jobOf('a3') }} />
<Story name="Failed" args={{ job: jobOf('a4') }} />
