<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import Checkbox from './checkbox.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Checkbox',
    component: Checkbox,
    // Without this the bare control renders unnamed and axe flags it.
    args: { 'aria-label': 'Auto-connect' },
  });
</script>

<Story name="Default" />

<Story name="All States" asChild>
  <div class="flex flex-wrap gap-4">
    <Checkbox aria-label="Unchecked" />
    <Checkbox aria-label="Checked" checked />
    <Checkbox aria-label="Some rows ticked" indeterminate />
    <Checkbox aria-label="Disabled" disabled />
    <Checkbox aria-label="Disabled and checked" disabled checked />
  </div>
</Story>

<Story
  name="Label Association Finds The Checkbox"
  tags={['!dev', '!autodocs']}
  asChild
>
  <div class="flex items-center gap-2">
    <Checkbox id="checkbox-with-label" checked />
    <label for="checkbox-with-label">Auto-connect</label>
  </div>
</Story>

<Story
  name="Toggles By Space And Aria Checked Follows"
  tags={['!dev', '!autodocs']}
/>

<Story
  name="Disabled Checkbox Is Not Reachable By Keyboard"
  tags={['!dev', '!autodocs']}
  args={{ disabled: true }}
/>
