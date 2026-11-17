<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import FacetPopover from '../facet-popover/facet-popover.svelte';
  import { facetListSummary } from '../facet-popover/summary';
  import SelectFacet from './select-facet.svelte';
  import type { SelectFacetOption } from './types';

  const { Story } = defineMeta({
    title: 'Primitives/SelectFacet',
    component: SelectFacet,
  });

  // The printers list's own two facets: the value is what the filter stores,
  // the count is what the facet counter returned for it.
  const STATES: SelectFacetOption[] = [
    { value: 'PRINTING', label: 'PRINTING', count: 6 },
    { value: 'IDLE', label: 'IDLE', count: 4 },
    { value: 'PAUSED', label: 'PAUSED', count: 2 },
    { value: 'ATTENTION', label: 'ATTENTION', count: 1 },
    { value: 'ERROR', label: 'ERROR', count: 0 },
    { value: 'OFFLINE', label: 'OFFLINE', count: 0 },
  ];

  const MODELS: SelectFacetOption[] = [
    { value: 'MK4S', label: 'MK4S', count: 6 },
    { value: 'MK4', label: 'MK4', count: 3 },
    { value: 'MK3.9', label: 'MK3.9', count: 2 },
    { value: 'MK3.5', label: 'MK3.5', count: 2 },
    { value: 'CORE One', label: 'CORE One', count: 4 },
    { value: 'XL 5T', label: 'XL 5T', count: 1 },
    { value: 'XL 2T', label: 'XL 2T', count: 1 },
    { value: 'XL', label: 'XL', count: 1 },
    { value: 'MINI+', label: 'MINI+', count: 2 },
    { value: 'MINI', label: 'MINI', count: 0 },
  ];

  const uncounted: SelectFacetOption[] = STATES.map((state) => ({
    value: state.value,
    label: state.label,
  }));
</script>

<script lang="ts">
  // Controlled, the way a screen uses it: the selection lives with whoever
  // owns the list, because the counts are derived from it. `onToggle` hands
  // back one value and the caller decides what the next selection is.
  const held: Record<string, string[]> = $state({
    states: [],
    types: [],
    selection: ['PRINTING', 'PAUSED'],
    withoutCounts: ['IDLE'],
    zero: [],
    miss: [],
    multiple: [],
  });

  let printer = $state('');

  const toggle = (key: string) => (value: string) => {
    const list = held[key] ?? [];
    held[key] = list.includes(value)
      ? list.filter((item) => item !== value)
      : [...list, value];
  };
</script>

<!-- The widths the printers list gives its two popovers: 230px for State,
     272px for the searchable Type. -->

<Story name="Default" asChild>
  <div
    class="rounded-md border border-border bg-popover p-2"
    style="width: 230px;"
  >
    <SelectFacet
      options={STATES}
      selected={held.states}
      onToggle={toggle('states')}
    />
  </div>
</Story>

<Story name="Searchable" asChild>
  <div
    class="rounded-md border border-border bg-popover p-2"
    style="width: 272px;"
  >
    <SelectFacet
      options={MODELS}
      searchable
      searchPlaceholder="Search type…"
      searchAriaLabel="Search printer types"
      selected={held.types}
      onToggle={toggle('types')}
    />
  </div>
</Story>

<Story name="With Selection" asChild>
  <div
    class="rounded-md border border-border bg-popover p-2"
    style="width: 230px;"
  >
    <SelectFacet
      options={STATES}
      selected={held.selection}
      onToggle={toggle('selection')}
    />
  </div>
</Story>

<Story name="Without Counts" asChild>
  <div
    class="rounded-md border border-border bg-popover p-2"
    style="width: 230px;"
  >
    <SelectFacet
      options={uncounted}
      selected={held.withoutCounts}
      onToggle={toggle('withoutCounts')}
    />
  </div>
</Story>

<Story name="Zero Count Stays But Is Disabled" asChild>
  <div
    class="rounded-md border border-border bg-popover p-2"
    style="width: 230px;"
  >
    <SelectFacet
      options={STATES}
      selected={held.zero}
      onToggle={toggle('zero')}
    />
  </div>
</Story>

<Story name="Search Finds Nothing" asChild>
  <div
    class="rounded-md border border-border bg-popover p-2"
    style="width: 272px;"
  >
    <SelectFacet
      options={MODELS}
      searchable
      searchPlaceholder="Search type…"
      searchAriaLabel="Search printer types"
      selected={held.miss}
      onToggle={toggle('miss')}
    />
  </div>
</Story>

<Story name="Single Mode" asChild>
  <FacetPopover
    label="Printer"
    summary={printer === '' ? undefined : printer}
    width={240}
  >
    <SelectFacet
      mode="single"
      allLabel="All printers"
      allCount={22}
      options={MODELS}
      selected={printer === '' ? [] : [printer]}
      onToggle={(value: string) => {
        printer = value;
      }}
    />
  </FacetPopover>
</Story>

<Story name="Multiple Mode" asChild>
  <FacetPopover
    label="Type"
    summary={facetListSummary(held.multiple ?? [])}
    count={(held.multiple ?? []).length}
    width={240}
  >
    <SelectFacet
      options={MODELS}
      selected={held.multiple}
      onToggle={toggle('multiple')}
    />
  </FacetPopover>
</Story>
