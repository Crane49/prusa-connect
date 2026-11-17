<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { ReleaseActs } from '../../release-acts.svelte';
  import { releaseRowsOf } from '../../releases';
  import { adminSlicers, t, viewOf } from '../../test/fakes';
  import ReleaseActions from './release-actions.svelte';

  const { Story } = defineMeta({
    title: 'Server slicing/ReleaseActions',
    component: ReleaseActions,
    args: {
      t,
      acts: new ReleaseActs(viewOf('ready', adminSlicers)),
      detailHref: '?release=2.9.6',
    },
  });

  const rows = releaseRowsOf(adminSlicers);
  const rowOf = (version: string) => {
    const found = rows.find((row) => row.version === version);
    if (!found) throw new Error(`no fake release ${version}`);
    return found;
  };
</script>

<Story
  name="Installed, administrator"
  args={{ row: rowOf('2.9.6'), administrator: true }}
/>
<Story
  name="Not installed, administrator"
  args={{ row: rowOf('2.9.2'), administrator: true }}
/>
<Story name="Reader" args={{ row: rowOf('2.9.6'), administrator: false }} />
