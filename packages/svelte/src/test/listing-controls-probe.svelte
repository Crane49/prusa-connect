<!-- A listing toolbar that holds its own view and its own visible fields, for
     the listing-controls tests. -->
<script lang="ts">
  import type { ListField, VisibleFields } from '../primitives/field-picker';
  import { ListingControls } from '../primitives/listing-controls';
  import { viewModes } from '../primitives/view-mode';

  let {
    withFields = true,
    withChildren = false,
    label,
  }: {
    /** Whether the listing offers fields to put away at all. */
    withFields?: boolean;
    withChildren?: boolean;
    /** The view group's own name, where the default is not specific enough. */
    label?: string;
  } = $props();

  const columns: ListField[] = [
    { id: 'file', label: 'File', required: true },
    { id: 'printer', label: 'Printer' },
  ];

  let visible = $state(['file', 'printer']);

  const held: VisibleFields = {
    get visible() {
      return visible;
    },
    onToggle: (id: string) => {
      visible = visible.includes(id)
        ? visible.filter((kept) => kept !== id)
        : [...visible, id];
    },
    onReset: () => {
      visible = ['file', 'printer'];
    },
  };

  let drawn = $state('table');
</script>

<ListingControls
  fields={withFields ? columns : undefined}
  fieldState={withFields ? held : undefined}
  view={{
    value: drawn,
    onValueChange: (next: string) => (drawn = next),
    options: viewModes(['table', 'rows']),
    label,
  }}
>
  {#if withChildren}<span data-testid="beside">12 jobs</span>{/if}
</ListingControls>
<p data-testid="drawn">{drawn}</p>
