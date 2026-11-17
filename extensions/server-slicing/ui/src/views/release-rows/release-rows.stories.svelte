<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { ReleaseActs } from '../../release-acts.svelte';
  import { releaseRowsOf } from '../../releases';
  import {
    adminSlicers,
    installingSlicers,
    longSlicers,
    t,
    viewOf,
  } from '../../test/fakes';
  import ReleaseRows from './release-rows.svelte';

  const { Story } = defineMeta({
    title: 'Server slicing/ReleaseRows',
    component: ReleaseRows,
    args: {
      t,
      shows: () => true,
      hrefOf: () => '?release=x',
      administrator: true,
      acts: new ReleaseActs(viewOf('ready', adminSlicers)),
    },
  });
</script>

<Story name="Versions" args={{ rows: releaseRowsOf(adminSlicers) }} />
<Story name="Installing" args={{ rows: releaseRowsOf(installingSlicers) }} />
<Story name="Long names" args={{ rows: releaseRowsOf(longSlicers) }} />
