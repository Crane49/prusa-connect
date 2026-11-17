<!--
  One setting that takes one of a few words, as a row of buttons with a label
  over it. The library's toggle group gives the keyboard and the pressed
  state, so every choice in every form behaves the same.
-->
<script lang="ts" generics="T extends string">
  import { Label, ToggleGroup, ToggleGroupItem } from '../../../primitives';

  let {
    label,
    value,
    options,
    onchange,
  }: {
    label: string;
    value: T;
    options: readonly { value: T; label: string }[];
    onchange: (next: T) => void;
  } = $props();

  const id = $props.id();

  /** Hands on the option a person pressed, typed as the options are. */
  const pressed = (next: string): void => {
    const option = options.find((item) => item.value === next);
    if (option !== undefined) onchange(option.value);
  };
</script>

<div class="grid gap-2">
  <Label {id}>{label}</Label>
  <ToggleGroup
    type="single"
    variant="outline"
    size="sm"
    aria-labelledby={id}
    class="flex-wrap justify-start"
    {value}
    onValueChange={pressed}
  >
    {#each options as option (option.value)}
      <ToggleGroupItem value={option.value}>{option.label}</ToggleGroupItem>
    {/each}
  </ToggleGroup>
</div>
