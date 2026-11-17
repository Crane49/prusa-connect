<!-- A toolbar over a listing that holds its own filters and its own fields,
     for the toolbar tests. -->
<script lang="ts">
  import type { ListField } from '../primitives/field-picker';
  import type { FacetDimension } from '../primitives/listing-state';
  import ListingToolbar from '../primitives/listing-toolbar/listing-toolbar.svelte';
  import { viewModes } from '../primitives/view-mode';

  const FIELDS: ListField[] = [
    { id: 'name', label: 'Name', required: true },
    { id: 'state', label: 'State' },
  ];

  let visible = $state<string[]>(['name', 'state']);
  let view = $state('cards');
  let picked = $state<string[]>([]);
  let flagged = $state(false);

  let {
    mode = 'multiple',
    filtersActive,
    onClearFilters,
    onSelectedChange,
  }: {
    mode?: 'multiple' | 'single';
    filtersActive?: boolean;
    onClearFilters?: () => void;
    onSelectedChange?: (next: string[]) => void;
  } = $props();

  const facets = $derived<FacetDimension[]>([
    {
      id: 'state',
      label: 'State',
      values: ['PRINTING', 'IDLE'],
      counts: new Map([
        ['PRINTING', 2],
        ['IDLE', 1],
      ]),
      selected: picked,
      onSelectedChange: (next: string[]) => {
        picked = next;
        onSelectedChange?.(next);
      },
      mode,
      allLabel: 'Any state',
      labelFor: (value: string) => (value === 'IDLE' ? 'Idle' : 'Printing'),
    },
  ]);
</script>

<ListingToolbar
  {facets}
  toggles={[
    {
      id: 'flag',
      label: 'Flagged',
      pressed: flagged,
      onPressedChange: (on: boolean) => (flagged = on),
    },
  ]}
  {filtersActive}
  {onClearFilters}
  fields={FIELDS}
  fieldState={{
    visible,
    onToggle: (id: string) => {
      visible = visible.includes(id)
        ? visible.filter((held) => held !== id)
        : [...visible, id];
    },
    onReset: () => (visible = ['name', 'state']),
  }}
  view={{
    value: view,
    options: viewModes(['cards', 'table']),
    onValueChange: (next: string) => (view = next),
  }}
/>

<p data-testid="picked">{picked.join(',')}</p>
<p data-testid="flagged">{flagged}</p>
