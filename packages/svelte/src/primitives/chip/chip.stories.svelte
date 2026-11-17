<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import Chip from './chip.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Chip',
    component: Chip,
  });
</script>

{#snippet text()}PLA{/snippet}

<Story name="Default" args={{ children: text }} />

<Story name="Selected" args={{ children: text, selected: true }} />

<Story name="With Count" args={{ children: text, count: 12 }} />

<Story
  name="Selected With Count"
  args={{ children: text, count: 12, selected: true }}
/>

<Story name="Link" args={{ children: text, href: '#pla', count: 3 }} />

<Story
  name="Current Link"
  args={{ children: text, href: '#pla', count: 3, selected: true }}
/>
