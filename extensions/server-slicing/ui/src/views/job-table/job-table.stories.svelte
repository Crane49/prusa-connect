<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { DEFAULT_JOB_SORT } from '../../jobs';
  import { everybodysJobs, longJobs, someJobs, t } from '../../test/fakes';
  import JobTable from './job-table.svelte';

  const { Story } = defineMeta({
    title: 'Server slicing/JobTable',
    component: JobTable,
    args: {
      t,
      shows: () => true,
      sort: DEFAULT_JOB_SORT,
      onSort: () => undefined,
      hrefOf: () => '?job=x',
      onCancel: () => undefined,
    },
  });
</script>

<Story name="Own jobs" args={{ jobs: someJobs.jobs, all: false }} />
<Story name="Every job" args={{ jobs: everybodysJobs.jobs, all: true }} />
<Story name="Long names" args={{ jobs: longJobs.jobs, all: true }} />
