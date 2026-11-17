<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { ReleaseActs } from '../../release-acts.svelte';
  import { releaseRowsOf } from '../../releases';
  import {
    adminSlicers,
    installingSlicers,
    linuxSlicers,
    someSlicers,
    t,
    troubledSlicers,
    viewOf,
  } from '../../test/fakes';
  import ReleaseDetail from './release-detail.svelte';

  const { Story } = defineMeta({
    title: 'Server slicing/ReleaseDetail',
    component: ReleaseDetail,
    args: {
      t,
      platform: 'darwin/arm64',
      administrator: true,
      acts: new ReleaseActs(viewOf('ready', adminSlicers)),
      backHref: '?',
    },
  });

  const rowOf = (slicers: typeof adminSlicers, version: string) =>
    releaseRowsOf(slicers).find((row) => row.version === version);
</script>

<Story
  name="Installed"
  args={{ version: '2.9.6', row: rowOf(adminSlicers, '2.9.6') }}
/>
<Story
  name="Local binary"
  args={{ version: '2.9.5', row: rowOf(adminSlicers, '2.9.5') }}
/>
<Story
  name="Without a checksum"
  args={{ version: '2.9.2', row: rowOf(adminSlicers, '2.9.2') }}
/>
<Story
  name="Installing"
  args={{
    version: '3.0.0-alpha12',
    row: rowOf(installingSlicers, '3.0.0-alpha12'),
  }}
/>
<Story
  name="Install failed"
  args={{
    version: '3.0.0-alpha12',
    row: rowOf(troubledSlicers, '3.0.0-alpha12'),
  }}
/>
<Story
  name="Not for this server"
  args={{
    version: '2.9.6',
    row: rowOf(linuxSlicers, '2.9.6'),
    platform: 'linux/arm64',
  }}
/>
<Story
  name="Reader"
  args={{
    version: '2.9.6',
    row: rowOf(someSlicers, '2.9.6'),
    administrator: false,
  }}
/>
<Story name="Not listed" args={{ version: '1.0.0', row: undefined }} />
