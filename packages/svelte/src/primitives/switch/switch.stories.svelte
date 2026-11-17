<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { Label } from '../label';
  import Switch from './switch.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Switch',
    component: Switch,
    // Without this the bare control renders unnamed and axe flags it.
    args: { 'aria-label': 'Auto-connect' },
  });
</script>

<Story name="Default" />

<Story name="All States" asChild>
  <div class="flex flex-wrap items-center gap-4">
    <Switch aria-label="Unchecked" />
    <Switch aria-label="Checked" checked />
    <Switch aria-label="Disabled" disabled />
    <Switch aria-label="Disabled and checked" disabled checked />
    <Switch aria-label="Small" size="sm" />
    <Switch aria-label="Small, checked" size="sm" checked />
    <Switch aria-label="Small, disabled" size="sm" disabled />
    <Switch
      aria-label="Small, disabled and checked"
      size="sm"
      disabled
      checked
    />
  </div>
</Story>

<Story
  name="Label Association Finds The Switch"
  tags={['!dev', '!autodocs']}
  asChild
>
  <div class="flex items-center gap-2">
    <Switch id="switch-with-label" checked />
    <Label for="switch-with-label">Auto-connect</Label>
  </div>
</Story>

<Story
  name="Toggles By Space And Aria Checked Follows"
  tags={['!dev', '!autodocs']}
/>

<Story
  name="Disabled Switch Is Not Reachable By Keyboard"
  tags={['!dev', '!autodocs']}
  args={{ disabled: true }}
/>
