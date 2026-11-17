<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import type { ListField } from '../field-picker';
  import Label from '../label/label.svelte';
  import ListingSearch from '../listing-search/listing-search.svelte';
  import type { FacetDimension } from '../listing-state';
  import Select from '../select/select.svelte';
  import SelectContent from '../select/select-content.svelte';
  import SelectItem from '../select/select-item.svelte';
  import SelectTrigger from '../select/select-trigger.svelte';
  import SelectValue from '../select/select-value.svelte';
  import Switch from '../switch/switch.svelte';
  import { viewModes } from '../view-mode';
  import ListingToolbar from './listing-toolbar.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/ListingToolbar',
    component: ListingToolbar,
  });

  const FIELDS: ListField[] = [
    { id: 'name', label: 'Name', required: true },
    { id: 'state', label: 'State' },
    { id: 'job', label: 'Job' },
  ];
</script>

<script lang="ts">
  let visible = $state<string[]>(['name', 'state', 'job']);
  let view = $state('cards');
  let states = $state<string[]>([]);
  let models = $state<string[]>([]);
  let attention = $state(false);
  let picked = $state<string[]>(['PRINTING']);
  let query = $state('');
  let order = $state('relevance');
  let flags = $state<Record<string, boolean>>({});
  let live = $state(true);

  const fieldState = $derived({
    visible,
    onToggle: (id: string) => {
      visible = visible.includes(id)
        ? visible.filter((held) => held !== id)
        : [...visible, id];
    },
    onReset: () => (visible = ['name', 'state', 'job']),
  });

  const viewState = $derived({
    value: view,
    options: viewModes(['cards', 'table']),
    onValueChange: (next: string) => (view = next),
  });

  const two = $derived<FacetDimension[]>([
    {
      id: 'state',
      label: 'State',
      values: ['PRINTING', 'IDLE', 'ERROR'],
      counts: new Map([
        ['PRINTING', 4],
        ['IDLE', 7],
        ['ERROR', 1],
      ]),
      selected: states,
      onSelectedChange: (next: string[]) => (states = next),
    },
    {
      id: 'model',
      label: 'Type',
      values: ['MK4S', 'CORE One', 'XL'],
      counts: new Map([
        ['MK4S', 6],
        ['CORE One', 5],
        ['XL', 1],
      ]),
      selected: models,
      onSelectedChange: (next: string[]) => (models = next),
    },
  ]);

  const narrowed = $derived<FacetDimension[]>(
    two.map((facet) =>
      facet.id === 'state'
        ? {
            ...facet,
            selected: picked,
            onSelectedChange: (next: string[]) => (picked = next),
          }
        : facet,
    ),
  );

  // The materials catalogue's row: four single-value facets, three switches,
  // the search and an order on the right.
  let held = $state<Record<string, string[]>>({});
  const many = $derived<FacetDimension[]>(
    [
      { id: 'class', label: 'Printing technology', values: ['FFF', 'SLA'] },
      { id: 'type', label: 'Type', values: ['PLA', 'PETG', 'ASA'] },
      {
        id: 'brand',
        label: 'Manufacturer',
        values: ['Prusament', 'Fillamentum'],
      },
      { id: 'colour', label: 'Colour', values: ['Red', 'Blue'] },
    ].map((facet) => ({
      ...facet,
      mode: 'single' as const,
      selected: held[facet.id] ?? [],
      onSelectedChange: (next: string[]) =>
        (held = { ...held, [facet.id]: next }),
    })),
  );
  const toggles = $derived(
    [
      { id: 'favourites', label: 'Favourites' },
      { id: 'stock', label: 'In stock' },
      { id: 'custom', label: 'My custom' },
    ].map((flag) => ({
      ...flag,
      pressed: flags[flag.id] === true,
      onPressedChange: (on: boolean) => (flags = { ...flags, [flag.id]: on }),
    })),
  );
</script>

<!--
  Beyond the React set: React draws this row inside each screen, so nothing
  here is a port. Full fills every slot; Narrow is the same toolbar at 360px,
  where the facets wrap under the search and the Live switch and view switch
  hold the end of the first line.
-->

<Story name="With Filters" asChild>
  <div class="w-4xl">
    <ListingToolbar
      facets={two}
      fields={FIELDS}
      {fieldState}
      view={viewState}
    />
  </div>
</Story>

<Story name="Without Filters" asChild>
  <div class="w-4xl">
    <ListingToolbar fields={FIELDS} {fieldState} view={viewState} />
  </div>
</Story>

<Story name="Filters On" asChild>
  <div class="w-4xl">
    <ListingToolbar
      facets={narrowed}
      toggles={[
        {
          id: 'attention',
          label: 'Needs attention',
          pressed: attention,
          onPressedChange: (on: boolean) => (attention = on),
        },
      ]}
      fields={FIELDS}
      {fieldState}
      view={viewState}
    />
  </div>
</Story>

<Story name="Many Filters" asChild>
  <div class="w-full max-w-5xl">
    <ListingToolbar facets={many} {toggles}>
      {#snippet search()}
        <ListingSearch
          value={query}
          onValue={(next: string) => (query = next)}
          label="Search materials"
          placeholder="Search materials, brands or types…"
        />
      {/snippet}
      {#snippet beforeControls()}
        <Select
          value={order}
          items={[
            { value: 'relevance', label: 'Relevance' },
            { value: 'name', label: 'Name A–Z' },
          ]}
          onValueChange={(next: string) => (order = next)}
        >
          <SelectTrigger size="sm" class="min-w-40" aria-label="Order">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="relevance">Relevance</SelectItem>
            <SelectItem value="name">Name A–Z</SelectItem>
          </SelectContent>
        </Select>
      {/snippet}
    </ListingToolbar>
  </div>
</Story>

{#snippet searchBox()}
  <ListingSearch
    value={query}
    onValue={(next: string) => (query = next)}
    label="Search printers"
    placeholder="Search printers…"
  />
{/snippet}

{#snippet sortBox()}
  <Select
    value={order}
    items={[
      { value: 'relevance', label: 'Relevance' },
      { value: 'name', label: 'Name A–Z' },
    ]}
    onValueChange={(next: string) => (order = next)}
  >
    <SelectTrigger size="sm" class="min-w-40" aria-label="Order">
      <SelectValue />
    </SelectTrigger>
    <SelectContent>
      <SelectItem value="relevance">Relevance</SelectItem>
      <SelectItem value="name">Name A–Z</SelectItem>
    </SelectContent>
  </Select>
{/snippet}

{#snippet liveSwitch()}
  <div class="flex h-8 items-center gap-2">
    <Switch
      id="toolbar-live"
      checked={live}
      onCheckedChange={(on: boolean) => (live = on)}
    />
    <Label for="toolbar-live">Live</Label>
  </div>
{/snippet}

<Story name="Full" asChild>
  <div class="w-full max-w-5xl">
    <ListingToolbar
      search={searchBox}
      facets={two}
      {toggles}
      beforeControls={sortBox}
      trailing={liveSwitch}
      fields={FIELDS}
      {fieldState}
      view={viewState}
    />
  </div>
</Story>

<Story name="Narrow" asChild>
  <div class="w-90">
    <ListingToolbar
      search={searchBox}
      facets={two}
      {toggles}
      beforeControls={sortBox}
      trailing={liveSwitch}
      fields={FIELDS}
      {fieldState}
      view={viewState}
    />
  </div>
</Story>

<Story name="Search Only" asChild>
  <div class="w-4xl">
    <ListingToolbar search={searchBox} />
  </div>
</Story>
