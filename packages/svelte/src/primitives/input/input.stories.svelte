<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import Label from '../label/label.svelte';
  import Input from './input.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Input',
    component: Input,
    args: { placeholder: 'MK4S' },
  });
</script>

<Story name="Default" />

<Story name="All States" asChild>
  <div class="flex flex-col gap-3">
    <Input placeholder="Rest" />
    <Input aria-label="Filled" value="Typed value" />
    <Input placeholder="Disabled" disabled />
  </div>
</Story>

<Story
  name="Label Association Finds The Input"
  tags={['!dev', '!autodocs']}
  asChild
>
  <div class="flex flex-col gap-2">
    <Label for="input-with-label">Printer name</Label>
    <Input id="input-with-label" value="MK4S" />
  </div>
</Story>

<Story name="Typing Lands In The Value" tags={['!dev', '!autodocs']} />

<Story
  name="Disabled Input Is Not Reachable By Keyboard"
  tags={['!dev', '!autodocs']}
  args={{ disabled: true }}
/>

<!-- Beyond the React stories: the control scale itself, which is the one thing
     this input adds to the shadcn original. -->
<Story name="All Sizes" asChild>
  <div class="flex flex-col gap-3">
    <Input size="sm" placeholder="Small" />
    <Input size="md" placeholder="Medium" />
    <Input size="lg" placeholder="Large" />
  </div>
</Story>
