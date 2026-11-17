<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import Toggle from './toggle.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Toggle',
    component: Toggle,
  });
</script>

<Story name="Default">Bold</Story>

<Story name="All Variants And Sizes" asChild>
  <div class="flex flex-wrap items-center gap-3">
    <Toggle size="sm">Bold</Toggle>
    <Toggle size="md">Bold</Toggle>
    <Toggle size="lg">Bold</Toggle>
    <Toggle variant="outline">Italic</Toggle>
    <Toggle pressed>Underline</Toggle>
    <Toggle variant="outline" pressed>Strikethrough</Toggle>
    <Toggle disabled>Disabled</Toggle>
    <Toggle disabled pressed>Disabled, pressed</Toggle>
  </div>
</Story>
