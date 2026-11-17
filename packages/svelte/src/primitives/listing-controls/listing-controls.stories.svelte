<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import type { ListField, VisibleFields } from '../field-picker/types';
  import { viewModes } from '../view-mode/catalogue';
  import ListingControls from './listing-controls.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/ListingControls',
    component: ListingControls,
  });

  const columns: ListField[] = [
    { id: 'file', label: 'File', required: true },
    { id: 'printer', label: 'Printer' },
    { id: 'state', label: 'State' },
    { id: 'started', label: 'Started', hiddenByDefault: true },
  ];
</script>

<script lang="ts">
  // The toolbar is the whole component, so each story holds what its own
  // listing draws.
  const drawn = $state({ full: 'table', plain: 'cards', counted: 'rows' });

  let visible = $state(['file', 'printer', 'state']);

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
      visible = ['file', 'printer', 'state'];
    },
  };
</script>

<!--
  Beyond the React set. React ships no listing-controls.stories.tsx, so nothing
  here is a port. These are the states the props allow: the pair, the toggle on
  its own, and a screen that keeps a count beside them.
-->

<Story name="Default" asChild>
  <ListingControls
    fields={columns}
    fieldState={held}
    view={{
      value: drawn.full,
      onValueChange: (next: string) => (drawn.full = next),
      options: viewModes(['table', 'rows', 'cards']),
    }}
  />
</Story>

<Story name="Nothing To Put Away" asChild>
  <ListingControls
    view={{
      value: drawn.plain,
      onValueChange: (next: string) => (drawn.plain = next),
      options: viewModes(['cards', 'rows']),
    }}
  />
</Story>

<Story name="With A Count Beside It" asChild>
  <ListingControls
    fields={columns}
    fieldState={held}
    view={{
      value: drawn.counted,
      onValueChange: (next: string) => (drawn.counted = next),
      options: viewModes(['table', 'rows']),
      label: 'Job listing view',
    }}
  >
    <span class="text-xs text-muted-foreground">12 jobs</span>
  </ListingControls>
</Story>
