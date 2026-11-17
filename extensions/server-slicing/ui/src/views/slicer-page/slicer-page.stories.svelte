<!--
  Every state the Slicing page draws. A story opens on a tab, a filter, or a
  release or a job, through the address it is handed, the way a link does.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { PageAddress } from '../../page-address.svelte';
  import { DEFAULT_STATE, type PageState } from '../../page-state';
  import {
    adminSlicers,
    everybodysJobs,
    installingSlicers,
    linuxSlicers,
    longJobs,
    longSlicers,
    manyJobs,
    manySlicers,
    noJobs,
    noSlicers,
    offlineSlicers,
    someJobs,
    someSlicers,
    t,
    troubledSlicers,
    updateSlicers,
    viewOf,
  } from '../../test/fakes';
  import SlicerPage from './slicer-page.svelte';

  const { Story } = defineMeta({
    title: 'Server slicing/SlicerPage',
    component: SlicerPage,
    args: { t },
  });

  const at = (state: Partial<PageState>) =>
    new PageAddress(null, { ...DEFAULT_STATE, ...state });

  const phone = { viewport: { value: 'phone', isRotated: false } };
</script>

<Story name="Loading" args={{ view: viewOf('loading') }} />
<Story name="Failed" args={{ view: viewOf('failed') }} />
<Story name="Refused" args={{ view: viewOf('refused') }} />
<Story name="Empty" args={{ view: viewOf('ready', noSlicers, noJobs) }} />
<Story name="Reader" args={{ view: viewOf('ready', someSlicers, someJobs) }} />
<Story
  name="Administrator"
  args={{ view: viewOf('ready', adminSlicers, everybodysJobs) }}
/>
<Story
  name="Filtered to nothing"
  args={{
    view: viewOf('ready', adminSlicers, everybodysJobs),
    address: at({
      releases: { q: '4.0', channel: ['stable'], state: [], line: [] },
    }),
  }}
/>
<Story
  name="Filtered"
  args={{
    view: viewOf('ready', manySlicers, everybodysJobs),
    address: at({
      releases: { q: '', channel: ['stable'], state: [], line: ['2.9'] },
    }),
  }}
/>
<Story
  name="Installing"
  args={{ view: viewOf('ready', installingSlicers, everybodysJobs) }}
/>
<Story
  name="Install failed"
  args={{ view: viewOf('ready', troubledSlicers, everybodysJobs) }}
/>
<Story
  name="Update available"
  args={{ view: viewOf('ready', updateSlicers, everybodysJobs) }}
/>
<Story
  name="GitHub out of reach"
  args={{ view: viewOf('ready', offlineSlicers, everybodysJobs) }}
/>
<Story
  name="No build for this server"
  args={{ view: viewOf('ready', linuxSlicers, noJobs) }}
/>
<Story
  name="Many versions"
  args={{ view: viewOf('ready', manySlicers, everybodysJobs) }}
/>
<Story
  name="Long names"
  args={{ view: viewOf('ready', longSlicers, longJobs) }}
/>
<Story
  name="Phone"
  globals={phone}
  args={{ view: viewOf('ready', adminSlicers, everybodysJobs) }}
/>
<Story
  name="Action failed"
  args={{
    view: {
      ...viewOf('ready', adminSlicers, everybodysJobs),
      actionFault: {
        code: 'server-slicing/VERSION_BUSY',
        title: 'That version is being installed or is slicing',
        detail: 'a job is slicing with 2.9.6',
      },
    },
  }}
/>
<Story
  name="Release"
  args={{
    view: viewOf('ready', adminSlicers, everybodysJobs),
    address: at({ release: '2.9.6' }),
  }}
/>
<Story
  name="Release without a checksum"
  args={{
    view: viewOf('ready', adminSlicers, everybodysJobs),
    address: at({ release: '2.9.2' }),
  }}
/>
<Story
  name="Release installing"
  args={{
    view: viewOf('ready', installingSlicers, everybodysJobs),
    address: at({ release: '3.0.0-alpha12' }),
  }}
/>
<Story
  name="Release install failed"
  args={{
    view: viewOf('ready', troubledSlicers, everybodysJobs),
    address: at({ release: '3.0.0-alpha12' }),
  }}
/>
<Story
  name="Release not listed"
  args={{
    view: viewOf('ready', adminSlicers, everybodysJobs),
    address: at({ release: '1.0.0' }),
  }}
/>
<Story
  name="Release, phone"
  globals={phone}
  args={{
    view: viewOf('ready', longSlicers, everybodysJobs),
    address: at({
      release: '2.9.4-alpha1234567890-with-a-very-long-build-label',
    }),
  }}
/>
<Story
  name="Jobs"
  args={{
    view: viewOf('ready', adminSlicers, everybodysJobs),
    address: at({ tab: 'jobs' }),
  }}
/>
<Story
  name="No jobs"
  args={{
    view: viewOf('ready', adminSlicers, noJobs),
    address: at({ tab: 'jobs' }),
  }}
/>
<Story
  name="Jobs filtered to nothing"
  args={{
    view: viewOf('ready', adminSlicers, everybodysJobs),
    address: at({
      tab: 'jobs',
      jobs: { q: 'nothing like this', state: [], slicer: [], project: [] },
    }),
  }}
/>
<Story
  name="Many jobs"
  args={{
    view: viewOf('ready', adminSlicers, manyJobs),
    address: at({ tab: 'jobs' }),
  }}
/>
<Story
  name="Jobs, phone"
  globals={phone}
  args={{
    view: viewOf('ready', adminSlicers, longJobs),
    address: at({ tab: 'jobs' }),
  }}
/>
<Story
  name="Job done"
  args={{
    view: viewOf('ready', adminSlicers, everybodysJobs),
    address: at({ job: 'a3' }),
  }}
/>
<Story
  name="Job slicing"
  args={{
    view: viewOf('ready', adminSlicers, everybodysJobs),
    address: at({ job: 'a1' }),
  }}
/>
<Story
  name="Job failed with a log"
  args={{
    view: viewOf('ready', adminSlicers, longJobs),
    address: at({ job: 'b1' }),
  }}
/>
<Story
  name="Job not listed"
  args={{
    view: viewOf('ready', adminSlicers, everybodysJobs),
    address: at({ job: 'gone' }),
  }}
/>
<Story
  name="API"
  args={{
    view: viewOf('ready', adminSlicers, everybodysJobs),
    address: at({ tab: 'api' }),
  }}
/>
<Story
  name="API, phone"
  globals={phone}
  args={{
    view: viewOf('ready', adminSlicers, everybodysJobs),
    address: at({ tab: 'api' }),
  }}
/>
<Story
  name="API, dark"
  globals={{ theme: 'dark' }}
  args={{
    view: viewOf('ready', adminSlicers, everybodysJobs),
    address: at({ tab: 'api' }),
  }}
/>
