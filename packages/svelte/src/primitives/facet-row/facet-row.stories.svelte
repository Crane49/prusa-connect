<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import FacetRow from './facet-row.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/FacetRow',
    component: FacetRow,
  });

  const printers = [
    { id: 'core-one', name: 'CORE One', count: 24 },
    { id: 'mk4s', name: 'MK4S', count: 8 },
    {
      id: 'xl',
      name: 'XL 5T with the enclosure and the filament buffer',
      count: 3,
    },
    { id: 'mini', name: 'MINI+', count: 0 },
  ];
</script>

<script lang="ts">
  // A row that does not move when it is pressed shows half the component, so
  // each story holds its own pick.
  const picked = $state({
    one: false,
    off: true,
    plain: false,
    emptyOn: true,
  });

  const panel: Record<string, boolean> = $state({ 'core-one': true });
</script>

<!--
  Beyond the React set. React ships no facet-row.stories.tsx, so nothing here
  is a port. These are the states the props allow: with a count and without, on
  and off, the two ways a row becomes unpickable, and a panel of rows together.
-->

<Story name="Default" asChild>
  <div class="w-64">
    <FacetRow
      count={1284}
      checked={picked.one}
      onToggle={() => (picked.one = !picked.one)}
    >
      {#snippet label()}Ready{/snippet}
    </FacetRow>
  </div>
</Story>

<Story name="Picked" asChild>
  <div class="w-64">
    <FacetRow
      count={12}
      checked={picked.off}
      onToggle={() => (picked.off = !picked.off)}
    >
      {#snippet label()}Printing{/snippet}
    </FacetRow>
  </div>
</Story>

<Story name="Without A Count" asChild>
  <div class="w-64">
    <FacetRow
      checked={picked.plain}
      onToggle={() => (picked.plain = !picked.plain)}
    >
      {#snippet label()}Show finished jobs{/snippet}
    </FacetRow>
  </div>
</Story>

<Story name="Nothing To Find" asChild>
  <div class="w-64">
    <FacetRow count={0} checked={false} onToggle={() => undefined}>
      {#snippet label()}Attention{/snippet}
    </FacetRow>
  </div>
</Story>

<Story name="Nothing To Find And Picked" asChild>
  <div class="w-64">
    <FacetRow
      count={0}
      checked={picked.emptyOn}
      onToggle={() => (picked.emptyOn = !picked.emptyOn)}
    >
      {#snippet label()}Attention{/snippet}
    </FacetRow>
  </div>
</Story>

<Story name="Unpickable With A Hint" asChild>
  <div class="w-64">
    <FacetRow
      count={6}
      checked={false}
      disabled
      hint="read only"
      onToggle={() => undefined}
    >
      {#snippet label()}USB drive{/snippet}
    </FacetRow>
  </div>
</Story>

<Story name="A Panel Of Facets" asChild>
  <div class="w-64 rounded-md border border-border p-1">
    {#each printers as printer (printer.id)}
      <FacetRow
        count={printer.count}
        checked={panel[printer.id] ?? false}
        onToggle={() => (panel[printer.id] = !panel[printer.id])}
      >
        {#snippet label()}{printer.name}{/snippet}
      </FacetRow>
    {/each}
  </div>
</Story>
