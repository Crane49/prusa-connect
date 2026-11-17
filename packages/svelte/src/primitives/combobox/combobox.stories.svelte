<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import Combobox from './combobox.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Combobox',
    component: Combobox,
    args: {
      items: [
        { value: '1', label: 'Shelf A' },
        { value: '2', label: 'Shelf B' },
        { value: '3', label: 'Drybox' },
      ],
      value: undefined,
      onPick: () => undefined,
      label: 'Location',
      placeholder: 'Pick or type a location',
      class: 'max-w-xs',
    },
  });
</script>

<Story name="Empty" />

<Story name="Picked" args={{ value: '3' }} />

<Story
  name="With Create And None"
  args={{
    value: '1',
    onCreate: () => undefined,
    createLabel: (text: string) => `Create “${text}”`,
    noneLabel: 'No location',
  }}
/>

<Story name="Small" args={{ size: 'sm' }} />

<Story name="Large" args={{ size: 'lg' }} />

<Story name="No Items" args={{ items: [] }} />
