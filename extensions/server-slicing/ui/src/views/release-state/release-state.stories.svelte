<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { releaseRowsOf } from '../../releases';
  import {
    adminSlicers,
    installingSlicers,
    linuxSlicers,
    t,
    troubledSlicers,
  } from '../../test/fakes';
  import ReleaseState from './release-state.svelte';

  const { Story } = defineMeta({
    title: 'Server slicing/ReleaseState',
    component: ReleaseState,
    args: { t },
  });

  const rowOf = (rows: ReturnType<typeof releaseRowsOf>, version: string) => {
    const found = rows.find((row) => row.version === version);
    if (!found) throw new Error(`no fake release ${version}`);
    return found;
  };
</script>

<Story
  name="Installed"
  args={{ row: rowOf(releaseRowsOf(adminSlicers), '2.9.6') }}
/>
<Story
  name="Not installed"
  args={{ row: rowOf(releaseRowsOf(adminSlicers), '2.9.2') }}
/>
<Story
  name="Installing"
  args={{
    row: rowOf(releaseRowsOf(installingSlicers), '3.0.0-alpha12'),
    detailed: true,
  }}
/>
<Story
  name="Install failed"
  args={{ row: rowOf(releaseRowsOf(troubledSlicers), '3.0.0-alpha12') }}
/>
<Story
  name="Not for this server"
  args={{ row: rowOf(releaseRowsOf(linuxSlicers), '2.9.6') }}
/>
