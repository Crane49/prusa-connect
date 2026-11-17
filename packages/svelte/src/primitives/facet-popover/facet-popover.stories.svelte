<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import FacetRow from '../facet-row/facet-row.svelte';
  import FacetPopover from './facet-popover.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/FacetPopover',
    component: FacetPopover,
  });

  const states = [
    { id: 'ready', name: 'Ready', count: 24 },
    { id: 'printing', name: 'Printing', count: 8 },
    { id: 'attention', name: 'Attention', count: 3 },
  ];
</script>

<script lang="ts">
  const picked: Record<string, boolean> = $state({});
</script>

<!--
  Beyond the React set. React ships no facet-popover.stories.tsx, so nothing
  here is a port. These are the states the props allow: nothing picked, one
  picked, more picked than the summary lists, the three sizes, and a facet
  whose values are colours.
-->

{#snippet rows()}
  {#each states as state (state.id)}
    <FacetRow
      count={state.count}
      checked={picked[state.id] ?? false}
      onToggle={() => (picked[state.id] = !picked[state.id])}
    >
      {#snippet label()}{state.name}{/snippet}
    </FacetRow>
  {/each}
{/snippet}

<Story name="Nothing Picked" asChild>
  <FacetPopover label="State" width={240} children={rows} />
</Story>

<Story name="One Picked" asChild>
  <FacetPopover label="State" summary="Ready" width={240} children={rows} />
</Story>

<Story name="More Than The Summary Lists" asChild>
  <FacetPopover
    label="State"
    summary="Ready"
    count={4}
    width={240}
    children={rows}
  />
</Story>

<Story name="The Three Sizes" asChild>
  <div class="flex items-center gap-2">
    <FacetPopover label="State" size="sm" width={240} children={rows} />
    <FacetPopover label="State" size="md" width={240} children={rows} />
    <FacetPopover label="State" size="lg" width={240} children={rows} />
  </div>
</Story>

<Story name="Colours Words Cannot Summarise" asChild>
  <FacetPopover
    label="Colour"
    active
    swatches={['#f5a623', '#1b1b1f', '#6b6f73']}
    width={240}
    children={rows}
  />
</Story>
