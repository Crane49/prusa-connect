<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { ReleaseActs } from '../../release-acts.svelte';
  import { DEFAULT_RELEASE_SORT, releaseRowsOf } from '../../releases';
  import {
    adminSlicers,
    installingSlicers,
    longSlicers,
    t,
    viewOf,
  } from '../../test/fakes';
  import ReleaseTable from './release-table.svelte';

  const { Story } = defineMeta({
    title: 'Server slicing/ReleaseTable',
    component: ReleaseTable,
    args: {
      t,
      shows: () => true,
      sort: DEFAULT_RELEASE_SORT,
      onSort: () => undefined,
      hrefOf: () => '?release=x',
      administrator: true,
      acts: new ReleaseActs(viewOf('ready', adminSlicers)),
    },
  });
</script>

<Story name="Versions" args={{ rows: releaseRowsOf(adminSlicers) }} />
<Story name="Installing" args={{ rows: releaseRowsOf(installingSlicers) }} />
<Story name="Long names" args={{ rows: releaseRowsOf(longSlicers) }} />
