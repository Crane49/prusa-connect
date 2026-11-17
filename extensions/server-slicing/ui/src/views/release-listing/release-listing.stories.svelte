<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { PageAddress } from '../../page-address.svelte';
  import { DEFAULT_STATE } from '../../page-state';
  import { ReleaseActs } from '../../release-acts.svelte';
  import {
    adminSlicers,
    manySlicers,
    noSlicers,
    someSlicers,
    t,
    viewOf,
  } from '../../test/fakes';
  import ReleaseListing from './release-listing.svelte';

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
    title: 'Server slicing/ReleaseListing',
    component: ReleaseListing,
    args: {
      t,
      acts: new ReleaseActs(viewOf('ready', adminSlicers)),
      onCheck: () => undefined,
      onInstallUpdate: () => undefined,
    },
  });
</script>

<Story
  name="Administrator"
  args={{
    slicers: adminSlicers,
    administrator: true,
    address: new PageAddress(null),
    store: memory(),
  }}
/>
<Story
  name="Reader"
  args={{
    slicers: someSlicers,
    administrator: false,
    address: new PageAddress(null),
    store: memory(),
  }}
/>
<Story
  name="Empty"
  args={{
    slicers: noSlicers,
    administrator: true,
    address: new PageAddress(null),
    store: memory(),
  }}
/>
<Story
  name="Filtered to nothing"
  args={{
    slicers: adminSlicers,
    administrator: true,
    address: new PageAddress(null, {
      ...DEFAULT_STATE,
      releases: { q: 'none', channel: [], state: [], line: [] },
    }),
    store: memory(),
  }}
/>
<Story
  name="Many versions"
  args={{
    slicers: manySlicers,
    administrator: true,
    address: new PageAddress(null),
    store: memory(),
  }}
/>
