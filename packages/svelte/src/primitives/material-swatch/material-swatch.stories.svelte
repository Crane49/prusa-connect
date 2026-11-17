<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import MaterialSwatch from './material-swatch.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/MaterialSwatch',
    component: MaterialSwatch,
  });
</script>

<!-- Beyond the React set. React ships no material-swatch.stories.tsx. -->

<Story name="A Named Colour" asChild>
  <MaterialSwatch
    class="size-10"
    material={{ name: 'PETG Slate', colorName: 'Slate', colorRgba: '#6b6f73' }}
  />
</Story>

<Story name="A Colour With No Name" asChild>
  <MaterialSwatch
    class="size-10"
    material={{ name: 'PLA', colorRgba: '#1b1b1f' }}
  />
</Story>

<Story name="No Colour" asChild>
  <MaterialSwatch class="size-10" material={{ name: 'Generic ABS' }} />
</Story>
