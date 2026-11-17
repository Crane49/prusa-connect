<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import Label from './label.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Label',
    component: Label,
  });

  // The library has no Input yet, so the two stories below pair the label with
  // a plain styled input that keeps the same `id` and `disabled` wiring.
  const input =
    'rounded-sm border border-border bg-transparent px-3 py-1 text-sm';
</script>

<Story name="Default" asChild>
  <Label>Printer name</Label>
</Story>

<Story name="With Control" asChild>
  <div class="flex flex-col gap-2">
    <Label for="printer-name">Printer name</Label>
    <input id="printer-name" class={input} value="MK4S" />
  </div>
</Story>

<Story name="Disabled Peer" asChild>
  <div class="flex flex-col gap-2">
    <Label for="printer-name-disabled">Printer name</Label>
    <input id="printer-name-disabled" class={input} value="MK4S" disabled />
  </div>
</Story>

<Story name="Variants" asChild>
  <div class="flex flex-col gap-4">
    <Label>Default</Label>
    <div class="flex w-64 gap-2">
      <input id="option-box" type="checkbox" class="mt-0.5" />
      <Label variant="option" for="option-box">
        An option beside its checkbox, long enough to wrap onto a second line
      </Label>
    </div>
    <div class="flex flex-col gap-1">
      <Label variant="hint" for="hint-box">Hint over a dense input</Label>
      <input id="hint-box" class={input} />
    </div>
  </div>
</Story>
