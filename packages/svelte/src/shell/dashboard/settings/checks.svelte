<!--
  One setting that takes any of a few words: a group of boxes, each with its
  label, inside a fieldset that names the group. The answer keeps the order
  of `options`, whatever order the boxes were ticked in.
-->
<script lang="ts" generics="T extends string">
  import { Checkbox, Label } from '../../../primitives';

  let {
    legend,
    note,
    value,
    options,
    onchange,
  }: {
    legend: string;
    /** One line under the legend on what the choice means. */
    note?: string | undefined;
    value: readonly T[];
    options: readonly { value: T; label: string }[];
    onchange: (next: T[]) => void;
  } = $props();

  const id = $props.id();

  const toggled = (item: T, on: boolean): T[] =>
    options
      .map((option) => option.value)
      .filter((held) => (held === item ? on : value.includes(held)));

  /** The handler of one box. */
  const toggle =
    (item: T) =>
    (on: boolean): void => {
      onchange(toggled(item, on));
    };
</script>

<fieldset class="grid gap-2">
  <legend class="mbe-2 text-sm font-medium">{legend}</legend>
  {#if note !== undefined}
    <p class="text-xs text-muted-foreground">{note}</p>
  {/if}
  <div class="grid grid-cols-2 gap-2">
    {#each options as option (option.value)}
      <div class="flex items-center gap-2">
        <Checkbox
          id={`${id}-${option.value}`}
          checked={value.includes(option.value)}
          onCheckedChange={toggle(option.value)}
        />
        <Label for={`${id}-${option.value}`} variant="option">
          {option.label}
        </Label>
      </div>
    {/each}
  </div>
</fieldset>
