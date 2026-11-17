<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { Button } from '../button';
  import { Pager } from '../pager';
  import { ListingState } from './listing-state.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Listing State',
  });

  const ROWS = Array.from({ length: 63 }, (_, index) => ({
    id: index,
    name: `Printer ${String(index + 1).padStart(2, '0')}`,
  }));

  const stateOf = (shape: 'hero' | 'fleet') =>
    new ListingState({
      id: 'story',
      fields: [{ id: 'name', label: 'Name', required: true }],
      views: [],
      initialView: 'table',
      rows: () => ROWS,
      store: { read: () => null, write: () => undefined },
      compare: (a, b) => b.id - a.id,
      shape: () => shape,
    });
</script>

<!--
  A store, not a component: it cuts rows into pages, sorts them and holds the
  reader's page size. The stories draw its output as a plain list under the
  pager the listings use. Nothing is kept between visits.
-->

{#snippet listing(state: ListingState<(typeof ROWS)[number]>)}
  <div class="flex flex-col gap-3">
    <ul class="flex flex-col gap-1 text-sm">
      {#each state.page as row (row.id)}
        <li>{row.name}</li>
      {/each}
    </ul>
    {#if state.pager}
      <Pager {...state.pager} total={state.total} unit="printers" />
    {/if}
    <Button
      variant="secondary"
      size="sm"
      class="self-start"
      onclick={() => {
        state.setSort('name', state.sort?.order === 'asc' ? 'desc' : 'asc');
      }}
    >
      Sort: {state.sort?.order ?? 'newest first'}
    </Button>
  </div>
{/snippet}

<Story name="Paged Fleet" asChild>
  {@render listing(stateOf('fleet'))}
</Story>

<Story name="Hero Has No Pager" asChild>
  {@render listing(stateOf('hero'))}
</Story>
