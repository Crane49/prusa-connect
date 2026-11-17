<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { PageAddress } from '../../page-address.svelte';
  import { DEFAULT_STATE } from '../../page-state';
  import { everybodysJobs, manyJobs, noJobs, t } from '../../test/fakes';
  import JobListing from './job-listing.svelte';

  const memory = () => {
    const held: Record<string, string> = {};
    return {
      read: (key: string) => held[key] ?? null,
      write: (key: string, value: string) => {
        held[key] = value;
      },
    };
  };

  const { Story } = defineMeta({
    title: 'Server slicing/JobListing',
    component: JobListing,
    args: { t, onCancel: () => undefined },
  });
</script>

<Story
  name="Jobs"
  args={{
    jobs: everybodysJobs,
    address: new PageAddress(null),
    store: memory(),
  }}
/>
<Story
  name="Empty"
  args={{ jobs: noJobs, address: new PageAddress(null), store: memory() }}
/>
<Story
  name="Filtered to failed"
  args={{
    jobs: manyJobs,
    address: new PageAddress(null, {
      ...DEFAULT_STATE,
      jobs: { q: '', state: ['failed'], slicer: [], project: [] },
    }),
    store: memory(),
  }}
/>
<Story
  name="Many jobs"
  args={{ jobs: manyJobs, address: new PageAddress(null), store: memory() }}
/>
