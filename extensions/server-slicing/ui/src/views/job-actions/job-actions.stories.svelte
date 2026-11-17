<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { someJobs, t } from '../../test/fakes';
  import JobActions from './job-actions.svelte';

  const { Story } = defineMeta({
    title: 'Server slicing/JobActions',
    component: JobActions,
    args: { t, detailHref: '?job=a1', onCancel: () => undefined },
  });

  const jobOf = (id: string) => {
    const found = someJobs.jobs.find((job) => job.id === id);
    if (!found) throw new Error(`no fake job ${id}`);
    return found;
  };
</script>

<Story name="Slicing" args={{ job: jobOf('a1') }} />
<Story name="Done" args={{ job: jobOf('a3') }} />
