<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { everybodysJobs, longJobs, t } from '../../test/fakes';
  import JobRows from './job-rows.svelte';

  const { Story } = defineMeta({
    title: 'Server slicing/JobRows',
    component: JobRows,
    args: {
      t,
      shows: () => true,
      hrefOf: () => '?job=x',
      onCancel: () => undefined,
    },
  });
</script>

<Story name="Every job" args={{ jobs: everybodysJobs.jobs, all: true }} />
<Story name="Long names" args={{ jobs: longJobs.jobs, all: true }} />
